param(
  [switch]$Quiet,
  [ValidateSet("auto", "local", "docker")][string]$Runtime = "auto"
)
$ErrorActionPreference = "Continue"
if (-not $PSCommandPath) {
  Write-Error "[smoke] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)

$requests = (@(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"0"}}}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n") + "`n"

$binary = Join-Path $root "bin\hackathonkit-mcp.exe"
# auto keeps the historical local-first behaviour; local/docker force a runtime
# so a stale binary cannot silently bypass the container path.
$useLocal = switch ($Runtime) {
  "local"  { $true }
  "docker" { $false }
  default  {
    $exists = Test-Path -LiteralPath $binary
    if ($exists) {
      # Warn when the local binary predates the sources: auto would otherwise
      # silently test a stale artifact and never exercise the container path.
      $binTime = (Get-Item -LiteralPath $binary).LastWriteTime
      $newest = Get-ChildItem -Path (Join-Path $root "cmd"), (Join-Path $root "internal") -Recurse -Filter *.go -File -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTime -gt $binTime } |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1
      if ($newest -and -not $Quiet) {
        Write-Host "[smoke] warning: $binary is older than $($newest.FullName); the container path is not being exercised"
      }
    }
    $exists
  }
}
if (-not $Quiet) {
  Write-Host ("[smoke] runtime: " + $(if ($useLocal) { "local" } else { "docker" }))
}
if ($useLocal) {
  # Run with a bounded wait and inspect the exit code: a server that prints
  # three replies and then crashes (or hangs) must not pass.
  $inFile = [System.IO.Path]::GetTempFileName()
  $outFile = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -LiteralPath $inFile -Value $requests -NoNewline
    $proc = $null
    try {
      $proc = Start-Process -FilePath $binary -ArgumentList @("--repo-root", "`"$root`"") `
        -RedirectStandardInput $inFile -RedirectStandardOutput $outFile -PassThru -NoNewWindow
    } catch {
      if (-not $Quiet) { Write-Host "[smoke] failed to start the MCP binary: $($_.Exception.Message)" }
      exit 1
    }
    if (-not $proc) {
      if (-not $Quiet) { Write-Host "[smoke] failed to start the MCP binary" }
      exit 1
    }
    if (-not $proc.WaitForExit(60000)) {
      try {
        if (-not $proc.HasExited) { $proc.Kill() }
        $proc.WaitForExit()
      } catch {
        # The process exited between the timeout check and Kill; ignore.
      }
      if (-not $Quiet) { Write-Host "[smoke] MCP server timed out after 60s" }
      exit 1
    }
    if ($proc.ExitCode -ne 0) {
      if (-not $Quiet) { Write-Host "[smoke] MCP server exited with code $($proc.ExitCode)" }
      exit 1
    }
    $output = Get-Content -LiteralPath $outFile
  } finally {
    Remove-Item -LiteralPath $inFile, $outFile -Force -ErrorAction SilentlyContinue
  }
} else {
  $docker = Get-Command docker -ErrorAction SilentlyContinue
  if (-not $docker) {
    if (-not $Quiet) { Write-Host "[smoke] no MCP runtime available (build bin/ or start docker)" }
    exit 1
  }
  $inFile = [System.IO.Path]::GetTempFileName()
  $outFile = [System.IO.Path]::GetTempFileName()
  $composeFile = Join-Path $root "docker-compose.yml"
  # An explicit project name keeps the one-off run container addressable and
  # isolated from concurrent runs.
  $composeProject = "hackathonkit-smoke-$PID"
  try {
    Set-Content -LiteralPath $inFile -Value $requests -NoNewline
    $proc = $null
    try {
      $proc = Start-Process -FilePath "docker" -ArgumentList @("compose", "-p", $composeProject, "-f", "`"$composeFile`"", "run", "--rm", "-T", "hackathonkit-mcp") `
        -WorkingDirectory $root -RedirectStandardInput $inFile -RedirectStandardOutput $outFile -PassThru -NoNewWindow
    } catch {
      if (-not $Quiet) { Write-Host "[smoke] failed to start docker compose: $($_.Exception.Message)" }
      exit 1
    }
    if (-not $proc) {
      if (-not $Quiet) { Write-Host "[smoke] failed to start docker compose" }
      exit 1
    }
    if (-not $proc.WaitForExit(120000)) {
      try {
        if (-not $proc.HasExited) { $proc.Kill() }
        $proc.WaitForExit()
      } catch {
        # The process exited between the timeout check and Kill; ignore.
      }
      if (-not $Quiet) { Write-Host "[smoke] docker compose timed out after 120s" }
      exit 1
    }
    if ($proc.ExitCode -ne 0) {
      if (-not $Quiet) { Write-Host "[smoke] docker compose exited with code $($proc.ExitCode)" }
      exit 1
    }
    $output = Get-Content -LiteralPath $outFile
  } finally {
    Remove-Item -LiteralPath $inFile, $outFile -Force -ErrorAction SilentlyContinue
    # Tear the project down on every exit path: `run` starts the depends_on
    # services and creates the project network, so a success or non-zero exit
    # would otherwise leak postgres/minio and the network.
    & docker compose -p $composeProject -f $composeFile down --remove-orphans *> $null
  }
}

$lines = @($output | Where-Object { $_ -and $_.Trim() -ne "" })
$ok = $true
$ids = @()
$byId = @{}
foreach ($line in $lines) {
  try {
    $obj = $line | ConvertFrom-Json
    $ids += [string]$obj.id
    $byId[[string]$obj.id] = $obj
    if ($obj.error) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] JSON-RPC error for id=$($obj.id): $($obj.error.message)" }
    }
    $hasResult = $obj.PSObject.Properties.Name -contains "result"
    $hasError = $obj.PSObject.Properties.Name -contains "error"
    if (-not $hasResult -and -not $hasError) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] response id=$($obj.id) has no result payload" }
    }
    if ($hasResult -and $obj.result.isError) {
      $ok = $false
      if (-not $Quiet) { Write-Host "[smoke] tool call id=$($obj.id) reported isError" }
    }

  } catch {
    $ok = $false
    if (-not $Quiet) { Write-Host "[smoke] non-JSON stdout line detected: $line" }
  }
}
if ($lines.Count -ne 3) { $ok = $false }
# JSON-RPC does not guarantee response ordering: validate the id set (and reject
# duplicates), not the arrival sequence.
$idSet = @($ids | Sort-Object -Unique)
if ($ok -and (($idSet.Count -ne 3) -or (Compare-Object -ReferenceObject @("1", "2", "3") -DifferenceObject $idSet))) { $ok = $false }
if ($ok) {
  $init = $byId["1"].result
  if (-not $init -or -not $init.protocolVersion) {
    $ok = $false
    if (-not $Quiet) { Write-Host "[smoke] initialize result is missing protocolVersion" }
  }
  $tools = $byId["2"].result.tools
  if (-not $tools -or @($tools).Count -eq 0) {
    $ok = $false
    if (-not $Quiet) { Write-Host "[smoke] tools/list returned no tools" }
  }
  $ctx = $byId["3"].result
  if (-not $ctx -or -not $ctx.content -or @($ctx.content).Count -eq 0) {
    $ok = $false
    if (-not $Quiet) { Write-Host "[smoke] system.context returned no content" }
  }
}

if (-not $Quiet) {
  if ($ok) {
    Write-Host "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
  } else {
    Write-Host "[smoke] MCP handshake FAILED"
  }
}
if ($ok) { exit 0 } else { exit 1 }
