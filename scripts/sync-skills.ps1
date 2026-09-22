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
  Get-ChildItem -LiteralPath $src -Force | Copy-Item -Destination $tmp -Recurse -Force
  $srcCount = (Get-ChildItem -LiteralPath $src -Directory).Count
  $count = (Get-ChildItem -LiteralPath $tmp -Directory).Count
  if ($count -ne $srcCount) {
    Write-Error "[sync-skills] skill count mismatch: $srcCount source, $count copied"
    exit 1
  }
  if (Test-Path -LiteralPath $dst) { Remove-Item -LiteralPath $dst -Recurse -Force }
  Move-Item -LiteralPath $tmp -Destination $dst -Force
} finally {
  if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
}
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
exit 0
