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
  $Root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
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
$template = Get-Content -LiteralPath $templatePath -Raw -Encoding UTF8
if ([string]::IsNullOrWhiteSpace($template)) {
  Fail "[mcp-manifest] template is empty: $templatePath"
}

# Replace placeholders on the original text only: a root containing braces is data.
$rootLiteral = ConvertTo-Json -InputObject ($Root.Replace("\", "/")) -Compress
$escapedRoot = $rootLiteral.Substring(1, $rootLiteral.Length - 2)
$expanded = [regex]::Replace($template, '\{\{[^{}]+\}\}', {
  param($match)
  switch -CaseSensitive ($match.Value) {
    "{{REPO_ROOT}}" { return $escapedRoot }
    "{{MCP_LAUNCHER}}" { return "hackathonkit-mcp-launcher.js" }
    default { throw "[mcp-manifest] unknown template placeholder: $($match.Value)" }
  }
})
$parsed = $expanded | ConvertFrom-Json
$server = $parsed.mcpServers.hackathonkit
if (-not $server -or $server.command -isnot [string] -or $server.args -isnot [array]) {
  Fail "[mcp-manifest] invalid hackathonkit command or args in template"
}
$server.args = @($server.command) + $server.args
$server.command = "node"
$manifest = $parsed | ConvertTo-Json -Depth 100

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
        [System.IO.File]::Replace($manifestTmp, $manifestPath, [System.Management.Automation.Language.NullString]::Value)
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
