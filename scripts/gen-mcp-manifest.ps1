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
$manifest = $template.Replace("{{REPO_ROOT}}", $Root.Replace("\", "/")).Replace("{{MCP_LAUNCHER}}", "hackathonkit-mcp-launcher.cmd")
$manifestPath = Join-Path $Root "plugins\hackathonkit\.mcp.json"
$manifestTmp = "$manifestPath.tmp"
try {
  [System.IO.File]::WriteAllText($manifestTmp, $manifest, (New-Object System.Text.UTF8Encoding($false)))
  Move-Item -LiteralPath $manifestTmp -Destination $manifestPath -Force
} finally {
  if (Test-Path -LiteralPath $manifestTmp) { Remove-Item -LiteralPath $manifestTmp -Force -ErrorAction SilentlyContinue }
}
if ((Get-Content -LiteralPath $manifestPath -Raw) -match "\{\{") {
  Write-Error "[mcp-manifest] unexpanded placeholder remains in plugins/hackathonkit/.mcp.json"
  exit 1
}
Write-Host "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $Root"
