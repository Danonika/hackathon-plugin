param(
  [string]$Root
)
$ErrorActionPreference = "Stop"
if (-not $Root) {
  $Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}

$templatePath = Join-Path $Root "plugins\hackathonkit\.mcp.json.template"
if (-not (Test-Path -LiteralPath $templatePath)) {
  Write-Error "[mcp-manifest] template not found: $templatePath"
  exit 1
}
$template = Get-Content -LiteralPath $templatePath -Raw
$manifest = $template.Replace("{{REPO_ROOT}}", $Root.Replace("\", "/"))
$manifestPath = Join-Path $Root "plugins\hackathonkit\.mcp.json"
$manifestTmp = "$manifestPath.tmp"
try {
  [System.IO.File]::WriteAllText($manifestTmp, $manifest, (New-Object System.Text.UTF8Encoding($false)))
  Move-Item -LiteralPath $manifestTmp -Destination $manifestPath -Force
} finally {
  if (Test-Path -LiteralPath $manifestTmp) { Remove-Item -LiteralPath $manifestTmp -Force -ErrorAction SilentlyContinue }
}
Write-Host "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $Root"
