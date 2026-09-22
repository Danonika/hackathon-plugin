param(
  [string]$Root
)
$ErrorActionPreference = "Stop"

# Write-Error would terminate before the following exit under Stop, so report
# and exit explicitly.
function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

if (-not $Root) {
  if (-not $PSScriptRoot) {
    Fail "[mcp-manifest] cannot resolve the repository root (empty PSScriptRoot)"
  }
  $Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if (-not (Test-Path -LiteralPath $Root -PathType Container)) {
  Fail "[mcp-manifest] repository root not found: $Root"
}

$templatePath = Join-Path $Root "plugins\hackathonkit\.mcp.json.template"
if (-not (Test-Path -LiteralPath $templatePath)) {
  Fail "[mcp-manifest] template not found: $templatePath"
}
$template = Get-Content -LiteralPath $templatePath -Raw
if ($null -eq $template) {
  Fail "[mcp-manifest] template is empty: $templatePath"
}

# ConvertTo-Json produces a correctly escaped JSON string literal; strip the
# surrounding quotes so the value can be spliced into the template.
$rootJson = ($Root.Replace("\", "/") | ConvertTo-Json -Compress)
$rootJson = $rootJson.Substring(1, $rootJson.Length - 2)
$manifest = $template.Replace("{{REPO_ROOT}}", $rootJson).Replace("{{MCP_LAUNCHER}}", "hackathonkit-mcp-launcher.cmd")

$manifestPath = Join-Path $Root "plugins\hackathonkit\.mcp.json"
$manifestTmp = "$manifestPath.tmp.$PID"
try {
  # Validate the string we are about to write (not a re-read that could decode
  # differently from the UTF-8 no-BOM bytes on disk).
  if ($manifest -match "\{\{(REPO_ROOT|MCP_LAUNCHER)\}\}") {
    Fail "[mcp-manifest] unexpanded placeholder remains; keeping the previous manifest"
  }
  try {
    $null = $manifest | ConvertFrom-Json
  } catch {
    Fail "[mcp-manifest] rendered manifest is not valid JSON: $($_.Exception.Message)"
  }
  [System.IO.File]::WriteAllText($manifestTmp, $manifest, (New-Object System.Text.UTF8Encoding($false)))
  if (Test-Path -LiteralPath $manifestPath) {
    # Atomic replace: never leave the destination deleted if the rename fails.
    [System.IO.File]::Replace($manifestTmp, $manifestPath, $null)
  } else {
    [System.IO.File]::Move($manifestTmp, $manifestPath)
  }
} finally {
  if (Test-Path -LiteralPath $manifestTmp) { Remove-Item -LiteralPath $manifestTmp -Force -ErrorAction SilentlyContinue }
}
Write-Host "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $Root"
exit 0
