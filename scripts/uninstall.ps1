param(
  [switch]$Yes
)
$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)

if (Get-Command codex -ErrorAction SilentlyContinue) {
  if ($Yes) {
    cmd /c "codex plugin remove hackathonkit >nul 2>nul"
    cmd /c "codex plugin marketplace remove hackathonkit-local >nul 2>nul"
    Write-Host "[uninstall] removed hackathonkit plugin and marketplace"
  } else {
    Write-Host "would run: codex plugin remove hackathonkit"
    Write-Host "would run: codex plugin marketplace remove hackathonkit-local"
  }
}

$targets = @(
  "plugins\hackathonkit\.mcp.json",
  ".agents\skills",
  ".claude\skills",
  "configs\install-state.json",
  "data\control",
  "bin"
)
foreach ($rel in $targets) {
  if ($Yes) {
    Remove-Item -Recurse -Force (Join-Path $root $rel) -ErrorAction SilentlyContinue
  } else {
    Write-Host "would remove: $rel"
  }
}

if (-not $Yes) {
  Write-Host "[uninstall] dry run: re-run with -Yes to apply"
  exit 0
}
Write-Host "[uninstall] done. data/, logs/ and committed files were kept."
