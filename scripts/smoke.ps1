param([switch]$Quiet)
$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)

$requests = @(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"0"}}}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n"

$binary = Join-Path $root "bin\hackathonkit-mcp.exe"
if (Test-Path -LiteralPath $binary) {
  $output = $requests | & $binary --repo-root $root 2>$null
} else {
  $docker = Get-Command docker -ErrorAction SilentlyContinue
  if (-not $docker) {
    if (-not $Quiet) { Write-Host "[smoke] no MCP runtime available (build bin/ or start docker)" }
    exit 1
  }
  Push-Location $root
  $output = $requests | docker compose -f (Join-Path $root "docker-compose.yml") run --rm -T hackathonkit-mcp 2>$null
  Pop-Location
}

$lines = @($output | Where-Object { $_ -and $_.Trim() -ne "" })
$ok = $true
$ids = @()
$parsedCount = 0
foreach ($line in $lines) {
  try {
    $obj = $line | ConvertFrom-Json
    $parsedCount++
    $ids += [string]$obj.id
    if ($obj.error) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] JSON-RPC error for id=$($obj.id): $($obj.error.message)" }
    }
    if (-not $obj.result -and -not $obj.error) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] response id=$($obj.id) has no result payload" }
    }
    if ($obj.result -and $obj.result.isError) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] tool call id=$($obj.id) reported isError" }
    }
    if ([string]$obj.id -ne [string]$parsedCount) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] unexpected response order: expected id=$parsedCount, got id=$($obj.id)" }
    }
  } catch {
    $ok = $false
    if (-not $Quiet) { Write-Host "[smoke] non-JSON stdout line detected: $line" }
  }
}
if ($lines.Count -ne 3) { $ok = $false }
if ($ok -and (($ids -join ",") -ne "1,2,3")) { $ok = $false }

if (-not $Quiet) {
  if ($ok) {
    Write-Host "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
  } else {
    Write-Host "[smoke] MCP handshake FAILED"
  }
}
if ($ok) { exit 0 } else { exit 1 }
