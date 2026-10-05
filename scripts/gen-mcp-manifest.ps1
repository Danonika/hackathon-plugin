param(
  [string]$Root
)
$ErrorActionPreference = "Stop"

# Write-Error would terminate before the following exit under Stop, so report
# and exit explicitly.
function Fail([string]$Message) {
  [Console]::Error.WriteLine($Message)
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
# Canonicalize so a caller-supplied relative/`..` path cannot be spliced into
# the manifest as a relative path. Resolve-Path does not dereference reparse
# points on Windows PowerShell 5.1, so the result is absolute but may still be
# a link path.
$Root = (Resolve-Path -LiteralPath $Root).Path

$templatePath = Join-Path $Root "plugins/hackathonkit/.mcp.json.template"
if (-not (Test-Path -LiteralPath $templatePath)) {
  Fail "[mcp-manifest] template not found: $templatePath"
}
$template = Get-Content -LiteralPath $templatePath -Raw
if ([string]::IsNullOrWhiteSpace($template)) {
  Fail "[mcp-manifest] template is empty: $templatePath"
}

# ConvertTo-Json produces a correctly escaped JSON string literal; strip the
# surrounding quotes so the value can be spliced into the template.
$rootJson = ($Root.Replace("\", "/") | ConvertTo-Json -Compress)
$rootJson = $rootJson.Substring(1, $rootJson.Length - 2)
# Validate the TEMPLATE for unknown placeholders before substitution. Checking
# the rendered manifest instead would false-positive when the resolved root path
# legitimately contains braces as data (e.g. C:\work\{{demo}}\hackathonkit).
if ($template -match "\{\{") {
  $stripped = $template.Replace("{{REPO_ROOT}}", "").Replace("{{MCP_LAUNCHER}}", "")
  if ($stripped -match "\{\{") {
    Fail "[mcp-manifest] template contains an unknown placeholder ({{...}})"
  }
}
$manifest = $template.Replace("{{REPO_ROOT}}", $rootJson).Replace("{{MCP_LAUNCHER}}", "hackathonkit-mcp-launcher.cmd")

$manifestPath = Join-Path $Root "plugins/hackathonkit/.mcp.json"
$manifestTmp = "$manifestPath.tmp.$PID"
try {
  # Validate the string we are about to write (not a re-read that could decode
  # differently from the UTF-8 no-BOM bytes on disk). Unknown placeholders were
  # already rejected against the template; braces in the resolved root are data.
  try {
    $null = $manifest | ConvertFrom-Json
  } catch {
    Fail "[mcp-manifest] rendered manifest is not valid JSON: $($_.Exception.Message)"
  }
  [System.IO.File]::WriteAllText($manifestTmp, $manifest, (New-Object System.Text.UTF8Encoding($false)))
  # The destination can be created/removed between the probe and the rename;
  # retry once so a check-then-act race does not lose the update.
  $published = $false
  for ($attempt = 0; $attempt -lt 2 -and -not $published; $attempt++) {
    try {
      if (Test-Path -LiteralPath $manifestPath) {
        # Atomic replace: never leave the destination deleted if the rename fails.
        [System.IO.File]::Replace($manifestTmp, $manifestPath, $null)
      } else {
        [System.IO.File]::Move($manifestTmp, $manifestPath)
      }
      $published = $true
    } catch {
      if ($attempt -ge 1) { throw }
      Start-Sleep -Milliseconds 50
    }
  }
} finally {
  if (Test-Path -LiteralPath $manifestTmp) { Remove-Item -LiteralPath $manifestTmp -Force -ErrorAction SilentlyContinue }
}
Write-Host "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $Root"
exit 0
