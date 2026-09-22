param(
  [string]$Root
)
$ErrorActionPreference = "Stop"
if (-not $Root) {
  if (-not $PSCommandPath) {
    Write-Error "[mcp-manifest] cannot resolve the repository root from an empty PSCommandPath"
    exit 1
  }
  $Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if (-not (Test-Path -LiteralPath $Root -PathType Container)) {
  Write-Error "[mcp-manifest] repository root not found: $Root"
  exit 1
}

$templatePath = Join-Path $Root "plugins\hackathonkit\.mcp.json.template"
if (-not (Test-Path -LiteralPath $templatePath)) {
  Write-Error "[mcp-manifest] template not found: $templatePath"
  exit 1
}
$template = Get-Content -LiteralPath $templatePath -Raw

# ConvertTo-Json produces a correctly escaped JSON string literal; strip the
# surrounding quotes so the value can be spliced into the template.
$rootJson = ($Root.Replace("\", "/") | ConvertTo-Json -Compress)
$rootJson = $rootJson.Substring(1, $rootJson.Length - 2)
$manifest = $template.Replace("{{REPO_ROOT}}", $rootJson).Replace("{{MCP_LAUNCHER}}", "hackathonkit-mcp-launcher.cmd")

$manifestPath = Join-Path $Root "plugins\hackathonkit\.mcp.json"
$manifestTmp = "$manifestPath.tmp"
try {
  [System.IO.File]::WriteAllText($manifestTmp, $manifest, (New-Object System.Text.UTF8Encoding($false)))
  if ((Get-Content -LiteralPath $manifestTmp -Raw) -match "\{\{") {
    Write-Error "[mcp-manifest] unexpanded placeholder remains; keeping the previous manifest"
    exit 1
  }
  Move-Item -LiteralPath $manifestTmp -Destination $manifestPath -Force
} finally {
  if (Test-Path -LiteralPath $manifestTmp) { Remove-Item -LiteralPath $manifestTmp -Force -ErrorAction SilentlyContinue }
}
Write-Host "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $Root"
