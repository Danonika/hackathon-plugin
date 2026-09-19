$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)

$json = (cmd /c "codex plugin list --json 2>nul") -join "`n"
if (-not $json) {
  Write-Host "[plugin-check] codex plugin list is unavailable"
  exit 1
}
try {
  $parsed = $json | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse plugin list"
  exit 1
}
$entry = $parsed.installed | Where-Object { $_.name -eq "hackathonkit" } | Select-Object -First 1
if (-not $entry) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$mcpPath = Join-Path $entry.source.path ".mcp.json"
if (-not (Test-Path -LiteralPath $mcpPath)) {
  Write-Host "[plugin-check] .mcp.json is missing in the installed plugin; run scripts/setup.ps1"
  exit 1
}
$mcp = Get-Content -LiteralPath $mcpPath -Raw | ConvertFrom-Json
$server = $mcp.mcpServers.hackathonkit
if (-not $server) {
  Write-Host "[plugin-check] hackathonkit server is missing in .mcp.json"
  exit 1
}
$cmd = [string]$server.command
$argList = @($server.args | ForEach-Object { [string]$_ })

$temp = Join-Path $env:TEMP ("hackathonkit-plugin-check-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temp | Out-Null
$requests = @(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"plugin-check","version":"0"}}}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n"

Push-Location $temp
$out = $requests | & $cmd @argList 2>$null
Pop-Location
Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue

$lines = @($out | Where-Object { $_ -and $_.Trim() -ne "" })
if ($lines.Count -ne 3) {
  Write-Host "[plugin-check] FAIL: expected 3 JSON-RPC responses from a foreign working directory, got $($lines.Count)"
  exit 1
}
foreach ($line in $lines) {
  try {
    $null = $line | ConvertFrom-Json
  } catch {
    Write-Host "[plugin-check] FAIL: non-JSON output detected: $line"
    exit 1
  }
}
Write-Host "[plugin-check] ok: installed plugin starts the MCP from a foreign working directory"
exit 0
