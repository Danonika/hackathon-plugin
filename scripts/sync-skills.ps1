param([ValidateSet("core", "full")][string]$InstallProfile)
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

function Get-TreeManifest([string]$Path) {
  # Walk manually instead of `-Recurse`: Windows PowerShell 5.1 follows
  # directory reparse points (junctions/symlinks) during -Recurse, which would
  # pull in files from outside the tree. Skip any reparse-point entry entirely.
  $items = New-Object System.Collections.Generic.List[string]
  $stack = New-Object System.Collections.Stack
  $stack.Push($Path)
  while ($stack.Count -gt 0) {
    $dir = $stack.Pop()
    foreach ($entry in (Get-ChildItem -LiteralPath $dir -Force)) {
      if ($entry.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { continue }
      if ($entry.PSIsContainer) { $stack.Push($entry.FullName); continue }
      $rel = $entry.FullName.Substring($Path.Length).TrimStart('\', '/')
      $items.Add("$rel|$((Get-FileHash -LiteralPath $entry.FullName -Algorithm SHA256).Hash)")
    }
  }
  @($items | Sort-Object)
}

if (-not $PSCommandPath -or $MyInvocation.InvocationName -eq '.') {
  Fail "[sync-skills] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Fail "[sync-skills] refusing: '$root' does not look like the hackathonkit repository root"
}
. (Join-Path $PSScriptRoot "skill-profile.ps1")
if (-not $InstallProfile) { $InstallProfile = Get-InstalledSkillProfile $root }
$src = Join-Path (Join-Path $root "plugins") (Join-Path "hackathonkit" "skills")
$dst = Join-Path (Join-Path $root ".agents") "skills"

if (-not (Test-Path -LiteralPath $src)) {
  Fail "skills source not found: $src"
}

# Serialize concurrent runs (e.g. sync-harness.ps1 plus a manual invocation):
# both mutate the shared $dst during the swap.
. (Join-Path $PSScriptRoot "file-lock.ps1")
$lockHandle = Enter-MirrorLock $root "agents-skills.lock"
$tmp = $null
try {
  [void][IO.Directory]::CreateDirectory((Split-Path -Parent $dst))
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
  # All new sh and PowerShell writers hold this kernel lock from before staging.
  # Discard staging/backup directories abandoned by previous owners after restoring.
  Get-ChildItem -LiteralPath (Split-Path -Parent $dst) -Directory -Force |
    Where-Object { $_.Name -match '^skills[.](tmp|old)[.]([0-9]+|[a-fA-F0-9]{32})$' -and -not ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName -Recurse -Force }

  # Build the mirror in a temporary directory and swap it in, so consumers never
  # observe a missing or half-populated mirror when the copy fails.
  $tmp = "$dst.tmp.$PID"
  if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  Copy-NoReparse $src $tmp
  # Compare the full relative file set (path + hash), not just top-level names.
  if (Compare-Object -ReferenceObject (Get-TreeManifest $src) -DifferenceObject (Get-TreeManifest $tmp)) {
    Fail "[sync-skills] mirror content mismatch between source and copy"
  }
  # Apply the same reparse-point filter Copy-NoReparse uses, otherwise a symlinked
  # skill folder makes the source count exceed the copied count and fails
  # spuriously.
  $srcCount = @(Get-ChildItem -LiteralPath $src -Directory -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) }).Count
  $dstCount = @(Get-ChildItem -LiteralPath $tmp -Directory -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) }).Count
  if ($srcCount -ne $dstCount) {
    Fail "[sync-skills] mirror mismatch: source $srcCount, copied $dstCount"
  }
  Select-StagedSkillProfile $tmp $InstallProfile
  $count = @(Get-ChildItem -LiteralPath $tmp -Directory -Force).Count
  # Move the live mirror aside before the swap so a failure can restore it.
  $old = "$dst.old.$([Guid]::NewGuid().ToString('N'))"
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
  if ($tmp -and (Test-Path -LiteralPath $tmp)) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
  $lockHandle.Dispose()
}
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
exit 0
