$ErrorActionPreference = "Continue"

$global:LASTEXITCODE = 0
$json = (& codex plugin list --json 2>$null) -join "`n"
if ($LASTEXITCODE -ne 0 -or -not $json) {
  Write-Host "[plugin-check] codex plugin list is unavailable"
  exit 1
}
try {
  $parsed = $json | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse codex plugin list output: $($_.Exception.Message)"
  exit 1
}
if (-not $parsed.installed) {
  Write-Host "[plugin-check] unexpected codex plugin list shape (no installed array)"
  exit 1
}
$entry = @($parsed.installed) | Where-Object { $_.name -eq "hackathonkit" } | Select-Object -First 1
if (-not $entry) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$sourcePath = $entry.source.path
if (-not $sourcePath) {
  Write-Host "[plugin-check] installed plugin has no source.path; re-run scripts/setup.ps1"
  exit 1
}
$mcpPath = Join-Path $sourcePath ".mcp.json"
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
if (-not $server -or -not $server.command) {
  Write-Host "[plugin-check] hackathonkit server is missing in .mcp.json"
  exit 1
}
$cmd = [string]$server.command
$argList = @($server.args | ForEach-Object { [string]$_ })

$temp = Join-Path $env:TEMP ("hackathonkit-plugin-check-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temp | Out-Null
$requests = @(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"plugin-check","version":"0"}}}',
  '{"jsonrpc":"2.0","method":"notifications/initialized"}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n"

$stderrPath = Join-Path $temp "stderr.txt"
$out = @()
$exitCode = 0
try {
  Push-Location $temp
  try {
    $out = @($requests | & $cmd @argList 2>$stderrPath)
    $exitCode = $LASTEXITCODE
  } finally {
    Pop-Location
  }
} finally {
  $stderrText = ""
  if (Test-Path -LiteralPath $stderrPath) {
    $stderrText = ((Get-Content -LiteralPath $stderrPath -Tail 5 -ErrorAction SilentlyContinue) -join "`n")
  }
  Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}

if ($exitCode -ne 0) {
  Write-Host "[plugin-check] FAIL: MCP command exited with code $exitCode"
  if ($stderrText) { Write-Host "[plugin-check] server stderr: $stderrText" }
  exit 1
}

$responses = @{}
foreach ($line in @($out | Where-Object { $_ -and $_.Trim() -ne "" })) {
  try {
    $obj = $line | ConvertFrom-Json
  } catch {
    continue
  }
  if ($obj.id -in @(1, 2, 3)) {
    $responses[[string]$obj.id] = $obj
    if ($obj.error) {
      Write-Host "[plugin-check] FAIL: error response for id=$($obj.id): $($obj.error.message)"
      exit 1
    }
  }
}
foreach ($want in @(1, 2, 3)) {
  $obj = $responses[[string]$want]
  if (-not $obj) {
    Write-Host "[plugin-check] FAIL: missing JSON-RPC response for id=$want from a foreign working directory"
    if ($stderrText) { Write-Host "[plugin-check] server stderr: $stderrText" }
    exit 1
  }
  $hasResult = $obj.PSObject.Properties.Name -contains "result"
  if (-not $hasResult) {
    Write-Host "[plugin-check] FAIL: response for id=$want has no result"
    exit 1
  }
  if ($obj.result.isError) {
    Write-Host "[plugin-check] FAIL: tool call id=$want reported isError"
    exit 1
  }
}
Write-Host "[plugin-check] ok: installed plugin starts the MCP from a foreign working directory"
exit 0
