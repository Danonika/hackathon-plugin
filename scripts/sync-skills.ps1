$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

function Copy-NoReparse([string]$From, [string]$To) {
  New-Item -ItemType Directory -Force -Path $To | Out-Null
  Get-ChildItem -LiteralPath $From -Force | ForEach-Object {
    if ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { return }
    $dest = Join-Path $To $_.Name
    if ($_.PSIsContainer) { Copy-NoReparse $_.FullName $dest }
    else { Copy-Item -LiteralPath $_.FullName -Destination $dest -Force }
  }
}

if (-not $PSCommandPath) {
  Fail "[sync-skills] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Fail "[sync-skills] refusing: '$root' does not look like the hackathonkit repository root"
}
$src = Join-Path (Join-Path $root "plugins") (Join-Path "hackathonkit" "skills")
$dst = Join-Path (Join-Path $root ".agents") "skills"

if (-not (Test-Path -LiteralPath $src)) {
  Fail "skills source not found: $src"
}

# Serialize concurrent runs (e.g. sync-harness.ps1 plus a manual invocation):
# both mutate the shared $dst during the swap.
$lock = "$dst.lock"
# The lock lives under $dst's parent (".agents"), which may not exist on a fresh
# checkout; create it once up front so the acquire loop is not defeated by a
# missing parent.
$lockParent = Split-Path -Parent $lock
if (-not (Test-Path -LiteralPath $lockParent)) {
  New-Item -ItemType Directory -Force -Path $lockParent | Out-Null
}
$locked = $false
# Owner token written inside the lock; cleanup only removes a lock this run owns.
$lockToken = "$PID-$([System.Guid]::NewGuid().ToString('N'))"
for ($i = 0; $i -lt 100; $i++) {
  try {
    New-Item -ItemType Directory -Path $lock -ErrorAction Stop | Out-Null
    Set-Content -LiteralPath (Join-Path $lock "pid") -Value $PID -ErrorAction SilentlyContinue
    Set-Content -LiteralPath (Join-Path $lock "owner") -Value $lockToken -ErrorAction SilentlyContinue
    $locked = $true
    break
  } catch {
    if (Test-Path -LiteralPath $lock) {
      $holder = $null
      $pidFile = Join-Path $lock "pid"
      if (Test-Path -LiteralPath $pidFile) {
        $holder = Get-Content -LiteralPath $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1
      }
      $stale = $false
      $holderPid = 0
      if ($holder -and [int]::TryParse("$holder", [ref]$holderPid) -and $holderPid -gt 0) {
        # Only evict when the owning process is gone; a slow-but-live copy must
        # not have its lock stolen. An unparseable pid falls through to the age
        # check rather than throwing.
        if (-not (Get-Process -Id $holderPid -ErrorAction SilentlyContinue)) { $stale = $true }
      } else {
        # No usable pid: only evict when the lock is old enough to be abandoned.
        # Guard the stat so a vanished lock cannot throw out of the catch.
        try {
          $age = (Get-Date) - (Get-Item -LiteralPath $lock -Force -ErrorAction Stop).LastWriteTime
          if ($age.TotalSeconds -gt 60) { $stale = $true }
        } catch { $stale = $false }
      }
      if ($stale) {
        $stalePath = "$lock.stale.$PID"
        try {
          Move-Item -LiteralPath $lock -Destination $stalePath -Force -ErrorAction Stop
          # The stale lock was renamed atomically: only the winner proceeds.
          Remove-Item -LiteralPath $stalePath -Recurse -Force -ErrorAction SilentlyContinue
        } catch {}
        continue
      }
    }
    Start-Sleep -Milliseconds 200
  }
}
if (-not $locked) { Fail "[sync-skills] could not acquire the mirror lock at $lock" }

# Recover from a previously interrupted swap before touching $dst: if the live
# mirror is missing but an old copy survives, restore it.
if (-not (Test-Path -LiteralPath $dst)) {
  $recover = Get-ChildItem -LiteralPath (Split-Path -Parent $dst) -Directory -Force -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -like "$(Split-Path -Leaf $dst).old.*" } |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
  if ($recover) {
    Write-Host "[sync-skills] restoring $dst from interrupted swap $($recover.FullName)"
    Move-Item -LiteralPath $recover.FullName -Destination $dst -Force
  }
}
# Sweep orphaned temp/stale-swap directories left by crashed runs, but never
# remove artifacts that a live process still owns: the embedded PID is checked.
function Remove-IfOrphaned([System.IO.DirectoryInfo]$Dir) {
  $m = [regex]::Match($Dir.Name, '\.(?:tmp|old|lock\.stale)\.(\d+)$')
  if (-not $m.Success) { return }
  $owner = 0
  if ([int]::TryParse($m.Groups[1].Value, [ref]$owner) -and $owner -gt 0) {
    if (Get-Process -Id $owner -ErrorAction SilentlyContinue) { return }
  }
  Remove-Item -LiteralPath $Dir.FullName -Recurse -Force -ErrorAction SilentlyContinue
}
Get-ChildItem -LiteralPath (Split-Path -Parent $dst) -Directory -Force -ErrorAction SilentlyContinue |
  Where-Object { $_.Name -like "$(Split-Path -Leaf $dst).tmp.*" -or $_.Name -like "$(Split-Path -Leaf $dst).old.*" -or $_.Name -like "$(Split-Path -Leaf $dst).lock.stale.*" } |
  ForEach-Object { Remove-IfOrphaned $_ }

# Build the mirror in a temporary directory and swap it in, so consumers never
# observe a missing or half-populated mirror when the copy fails.
$tmp = "$dst.tmp.$PID"
if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Copy-NoReparse $src $tmp
  $srcNames = @(Get-ChildItem -LiteralPath $src -Directory -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    Select-Object -ExpandProperty Name | Sort-Object)
  $dstNames = @(Get-ChildItem -LiteralPath $tmp -Directory -Force |
    Select-Object -ExpandProperty Name | Sort-Object)
  if (Compare-Object -ReferenceObject $srcNames -DifferenceObject $dstNames) {
    Fail "[sync-skills] mirror mismatch: source $($srcNames.Count), copied $($dstNames.Count)"
  }
  $count = $dstNames.Count
  $srcCount = $srcNames.Count
  # Move the live mirror aside before the swap so a failure can restore it.
  $old = "$dst.old.$PID"
  if (Test-Path -LiteralPath $dst) { Move-Item -LiteralPath $dst -Destination $old -Force }
  try {
    Move-Item -LiteralPath $tmp -Destination $dst -Force
  } catch {
    if (Test-Path -LiteralPath $old) { Move-Item -LiteralPath $old -Destination $dst -Force }
    throw
  }
  # Only delete the backup once the live mirror is known to exist.
  if ((Test-Path -LiteralPath $dst) -and (Test-Path -LiteralPath $old)) {
    Remove-Item -LiteralPath $old -Recurse -Force -ErrorAction SilentlyContinue
  }
} finally {
  if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
  # Remove the lock only if this run still owns it: a stale-reclaimed lock may
  # now belong to another process.
  if (Test-Path -LiteralPath $lock) {
    $ownerFile = Join-Path $lock "owner"
    $owner = Get-Content -LiteralPath $ownerFile -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($owner -eq $lockToken) {
      Remove-Item -LiteralPath $lock -Recurse -Force -ErrorAction SilentlyContinue
    }
  }
}
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
exit 0
