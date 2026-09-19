$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
$src = Join-Path $root "plugins\hackathonkit\skills"
$dst = Join-Path $root ".agents\skills"

if (-not (Test-Path -LiteralPath $src)) {
  Write-Error "skills source not found: $src" -ErrorAction Continue
  exit 1
}
if (Test-Path -LiteralPath $dst) {
  Remove-Item -LiteralPath $dst -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $dst | Out-Null
Copy-Item -Path (Join-Path $src "*") -Destination $dst -Recurse -Force

$count = (Get-ChildItem -LiteralPath $dst -Directory).Count
$srcCount = (Get-ChildItem -LiteralPath $src -Directory).Count
Write-Host "[sync-skills] mirrored $count of $srcCount skills to .agents/skills"
if ($count -ne $srcCount) {
  Write-Error "[sync-skills] skill count mismatch" -ErrorAction Continue
  exit 1
}
exit 0
