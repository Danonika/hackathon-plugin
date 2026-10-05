$ErrorActionPreference = "Continue"

$codexCmd = Get-Command codex -CommandType Application -ErrorAction SilentlyContinue
if (-not $codexCmd) {
  Write-Host "[plugin-check] codex executable not found on PATH"
  exit 1
}
$json = (& codex plugin list --json 2>$null) -join "`n"
$codexExit = $LASTEXITCODE
if ($codexExit -ne 0 -or -not $json) {
  Write-Host "[plugin-check] codex plugin list is unavailable (exit $codexExit)"
  exit 1
}
try {
  $parsed = $json | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse codex plugin list output: $($_.Exception.Message)"
  exit 1
}
if (-not ($parsed.PSObject.Properties.Name -contains 'installed')) {
  Write-Host "[plugin-check] unexpected codex plugin list shape (no installed array)"
  exit 1
}
if ($null -eq $parsed.installed) {
  Write-Host "[plugin-check] unexpected codex plugin list shape (installed is null)"
  exit 1
}
$installed = if ($parsed.installed -is [System.Array]) { $parsed.installed } else { @($parsed.installed) }
if ($installed.Count -eq 0) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$entry = @($installed) | Where-Object { $_.name -eq "hackathonkit" } | Select-Object -First 1
if (-not $entry) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$sourceProperty = $entry.PSObject.Properties['source']
if (-not $sourceProperty -or -not $sourceProperty.Value -or -not ($sourceProperty.Value.PSObject.Properties.Name -contains 'path')) {
  Write-Host "[plugin-check] installed plugin entry has an unexpected shape (missing source.path); re-run scripts/setup.ps1"
  exit 1
}
$sourcePath = $sourceProperty.Value.path
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
try {
  if (-not [System.IO.Path]::IsPathRooted($cmd)) {
    $cmd = Join-Path $sourcePath $cmd
  }
  $cmdFull = [System.IO.Path]::GetFullPath($cmd)
  $sourceFullRoot = [System.IO.Path]::GetFullPath($sourcePath)
} catch {
  Write-Host "[plugin-check] cannot resolve the installed plugin command path: $($_.Exception.Message)"
  exit 1
}
if (-not (Test-Path -LiteralPath $cmdFull)) {
  Write-Host "[plugin-check] MCP command does not exist: $cmdFull"
  exit 1
}
# Resolve symlinks/junctions so a link inside the plugin cannot point outside it.
$cmdResolved = (Resolve-Path -LiteralPath $cmdFull).Path
$sourceResolved = (Resolve-Path -LiteralPath $sourceFullRoot).Path
$comparison = if ($env:OS -eq "Windows_NT") { [System.StringComparison]::OrdinalIgnoreCase } else { [System.StringComparison]::Ordinal }
$sourcePrefix = $sourceResolved.TrimEnd([System.IO.Path]::DirectorySeparatorChar, '/', '\') + [System.IO.Path]::DirectorySeparatorChar
if (-not $cmdResolved.StartsWith($sourcePrefix, $comparison)) {
  Write-Host "[plugin-check] refusing to execute a command outside the installed plugin: $cmdResolved"
  exit 1
}
$cmd = $cmdResolved

$temp = Join-Path $env:TEMP ("hackathonkit-plugin-check-" + [guid]::NewGuid().ToString("N"))
try {
  New-Item -ItemType Directory -Path $temp -ErrorAction Stop | Out-Null
} catch {
  Write-Host "[plugin-check] cannot create the temp directory ${temp}: $($_.Exception.Message)"
  exit 1
}
$requests = @(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"plugin-check","version":"0"}}}',
  '{"jsonrpc":"2.0","method":"notifications/initialized"}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n"

$stderrPath = Join-Path $temp "stderr.txt"
$codePath = Join-Path $temp "exitcode.txt"
$out = @()
$exitCode = -1
$exitCodeError = ""
$stderrText = ""
try {
  # Run in a job so a hung server cannot block the check forever.
  $job = Start-Job -ScriptBlock {
    param($exe, $exeArgs, $payload, $workDir, $errPath, $codePath)
    Set-Location -LiteralPath $workDir
    $payload | & $exe @exeArgs 2>$errPath
    # $LASTEXITCODE does not survive Receive-Job; persist it for the caller.
    $code = if ($null -eq $LASTEXITCODE) { "" } else { "$LASTEXITCODE" }
    Set-Content -LiteralPath $codePath -Value $code -ErrorAction Stop
  } -ArgumentList $cmd, $argList, $requests, $temp, $stderrPath, $codePath
  if (-not $job) {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host "[plugin-check] FAIL: could not start the MCP check job (job subsystem unavailable)"
    exit 1
  }
  if (-not (Wait-Job $job -Timeout 120)) {
    Stop-Job $job -ErrorAction SilentlyContinue
    Remove-Job $job -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host "[plugin-check] FAIL: MCP command timed out after 120s"
    exit 1
  }
  $out = @(Receive-Job $job)
  if ($job.State -ne "Completed") {
    $exitCodeError = "the job did not complete (state: $($job.State))"
  } elseif (-not (Test-Path -LiteralPath $codePath)) {
    # Missing side file: do not assume success.
    $exitCodeError = "the job did not report an exit code"
  } else {
    $captured = Get-Content -LiteralPath $codePath -ErrorAction SilentlyContinue | Select-Object -First 1
    $parsed = 0
    if ($null -eq $captured -or "$captured".Trim() -eq "") {
      $exitCodeError = "the job reported an empty exit code"
    } elseif (-not [int]::TryParse("$captured".Trim(), [ref]$parsed)) {
      $exitCodeError = "the job reported a non-numeric exit code: '$captured'"
    } else {
      $exitCode = $parsed
    }
  }
  Remove-Job $job -Force -ErrorAction SilentlyContinue
} finally {
  if (Test-Path -LiteralPath $stderrPath) {
    $stderrText = ((Get-Content -LiteralPath $stderrPath -Tail 5 -ErrorAction SilentlyContinue) -join "`n")
  }
  Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}

if ($exitCodeError) {
  Write-Host "[plugin-check] FAIL: MCP command result could not be verified: $exitCodeError"
  if ($stderrText) { Write-Host "[plugin-check] server stderr: $stderrText" }
  exit 1
}
if ($exitCode -ne 0) {
  Write-Host "[plugin-check] FAIL: MCP command exited with code $exitCode"
  if ($stderrText) { Write-Host "[plugin-check] server stderr: $stderrText" }
  exit 1
}

$responses = @{}
foreach ($line in @($out | Where-Object { $_ -and $_.Trim() -ne "" })) {
  # Frame strictly by line: drop progress/log noise and partial JSON instead of
  # letting it poison every later response.
  $obj = $null
  try { $obj = $line | ConvertFrom-Json } catch { $obj = $null }
  if ($null -eq $obj) { continue }
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
