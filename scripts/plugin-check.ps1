$ErrorActionPreference = "Continue"

$json = (cmd /c "codex plugin list --json 2>nul") -join "`n"
if (-not $json) {
  Write-Host "[plugin-check] codex plugin list is unavailable"
  exit 1
}
try {
  $parsed = $json | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse codex plugin list output: $($_.Exception.Message)"
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
try {
  $mcp = Get-Content -LiteralPath $mcpPath -Raw -Encoding UTF8 | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse .mcp.json: $($_.Exception.Message)"
  exit 1
}
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

$stderrPath = Join-Path $temp "stderr.txt"
$pushed = $false
try {
  Push-Location $temp
  $pushed = $true
  $out = $requests | & $cmd @argList 2>$stderrPath
  Pop-Location
  $pushed = $false
} finally {
  if ($pushed) { Pop-Location }
}
$stderrText = ""
if (Test-Path -LiteralPath $stderrPath) {
  $stderrText = ((Get-Content -LiteralPath $stderrPath -Tail 5 -ErrorAction SilentlyContinue) -join "`n")
}
Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue

$lines = @($out | Where-Object { $_ -and $_.Trim() -ne "" })
if ($lines.Count -ne 3) {
  Write-Host "[plugin-check] FAIL: expected 3 JSON-RPC responses from a foreign working directory, got $($lines.Count)"
  if ($stderrText) { Write-Host "[plugin-check] server stderr: $stderrText" }
  exit 1
}
$expect = 0
foreach ($line in $lines) {
  $expect++
  try {
    $obj = $line | ConvertFrom-Json
  } catch {
    Write-Host "[plugin-check] FAIL: non-JSON output detected: $line"
    exit 1
  }
  if ($obj.error) {
    Write-Host "[plugin-check] FAIL: error response for id=$($obj.id): $($obj.error.message)"
    exit 1
  }
  if ([string]$obj.id -ne [string]$expect -or -not $obj.result) {
    Write-Host "[plugin-check] FAIL: unexpected response for id=$expect"
    exit 1
  }
  if ($obj.result.isError) {
    Write-Host "[plugin-check] FAIL: tool call id=$($obj.id) reported isError"
    exit 1
  }
}
Write-Host "[plugin-check] ok: installed plugin starts the MCP from a foreign working directory"
exit 0
