$ErrorActionPreference = "Stop"
if (-not $PSCommandPath) {
  Write-Error "[sync-skills] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Write-Error "[sync-skills] refusing: '$root' does not look like the hackathonkit repository root"
  exit 1
}
$src = Join-Path $root "plugins\hackathonkit\skills"
$dst = Join-Path $root ".agents\skills"

if (-not (Test-Path -LiteralPath $src)) {
  Write-Error "skills source not found: $src" -ErrorAction Continue
  exit 1
}

# Build the mirror in a temporary directory and swap it in, so consumers never
# observe a missing or half-populated mirror when the copy fails.
$tmp = "$dst.tmp.$PID"
if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Get-ChildItem -LiteralPath $src -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    Copy-Item -Destination $tmp -Recurse -Force
  $srcNames = @(Get-ChildItem -LiteralPath $src -Directory -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    Select-Object -ExpandProperty Name | Sort-Object)
  $dstNames = @(Get-ChildItem -LiteralPath $tmp -Directory -Force |
    Select-Object -ExpandProperty Name | Sort-Object)
  if (Compare-Object -ReferenceObject $srcNames -DifferenceObject $dstNames) {
    Write-Error "[sync-skills] mirror mismatch: source $($srcNames.Count), copied $($dstNames.Count)"
    exit 1
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
}
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
exit 0
