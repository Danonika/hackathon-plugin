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
$locked = $false
for ($i = 0; $i -lt 100; $i++) {
  try {
    New-Item -ItemType Directory -Path $lock -ErrorAction Stop | Out-Null
    $locked = $true
    break
  } catch {
    if (Test-Path -LiteralPath $lock) {
      $age = (Get-Date) - (Get-Item -LiteralPath $lock -Force).LastWriteTime
      if ($age.TotalSeconds -gt 60) {
        Remove-Item -LiteralPath $lock -Recurse -Force -ErrorAction SilentlyContinue
        continue
      }
    }
    Start-Sleep -Milliseconds 200
  }
}
if (-not $locked) { Fail "[sync-skills] could not acquire the mirror lock at $lock" }

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
  if (Test-Path -LiteralPath $old) { Remove-Item -LiteralPath $old -Recurse -Force }
} finally {
  if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
  if (Test-Path -LiteralPath $lock) { Remove-Item -LiteralPath $lock -Recurse -Force -ErrorAction SilentlyContinue }
}
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
exit 0
