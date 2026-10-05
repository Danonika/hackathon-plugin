param(
  [string]$Root = (Split-Path -Parent (Split-Path -Parent $PSCommandPath)),
  [string]$BaseUrl = "http://127.0.0.1:8787"
)
$ErrorActionPreference = "Continue"
$fail = 0
function Check([string]$name, [bool]$ok, [string]$detail) {
  if ($ok) { Write-Host ("[ok]   {0,-34} {1}" -f $name, $detail) }
  else { Write-Host ("[FAIL] {0,-34} {1}" -f $name, $detail); $script:fail++ }
}

$exe = Join-Path $Root "bin\hackathonkit-mcp.exe"
if (-not (Test-Path $exe)) { Check "mcp binary" $false "run scripts/setup.ps1"; exit 1 }
Check "mcp binary" $true $exe

$script:McpStderr = ""
# Native invocations run under a bounded wait so a hung/slow process (or a
# network stall) fails the run instead of blocking it forever.
function Invoke-ExeBounded([string[]]$ExeArgs, [string]$InFile, [string]$OutFile, [string]$ErrFile, [int]$TimeoutMs) {
  $proc = $null
  try {
    $proc = Start-Process -FilePath $exe -ArgumentList $ExeArgs `
      -RedirectStandardInput $InFile -RedirectStandardOutput $OutFile -RedirectStandardError $ErrFile -PassThru -NoNewWindow
  } catch {
    return $false
  }
  if (-not $proc) { return $false }
  if (-not $proc.WaitForExit($TimeoutMs)) {
    try {
      if (-not $proc.HasExited) { $proc.Kill() }
      $proc.WaitForExit()
    } catch { }
    return $false
  }
  return $true
}
function Mcp([string]$payload) {
  $inFile = [System.IO.Path]::GetTempFileName()
  $outFile = [System.IO.Path]::GetTempFileName()
  $errFile = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -LiteralPath $inFile -Value $payload
    if (-not (Invoke-ExeBounded -ExeArgs @("--repo-root", "`"$Root`"") -InFile $inFile -OutFile $outFile -ErrFile $errFile -TimeoutMs 60000)) {
      $script:McpStderr = "MCP call timed out after 60s"
      return ""
    }
    $script:McpStderr = (Get-Content -LiteralPath $errFile -Raw -ErrorAction SilentlyContinue)
    return ((Get-Content -LiteralPath $outFile -Raw -ErrorAction SilentlyContinue))
  } finally {
    Remove-Item -LiteralPath $inFile, $outFile, $errFile -Force -ErrorAction SilentlyContinue
  }
}
function Call([string]$id, [string]$tool, [string]$argsJson) {
  $req = '{"jsonrpc":"2.0","id":' + $id + ',"method":"tools/call","params":{"name":"' + $tool + '","arguments":' + $argsJson + '}}'
  $raw = Mcp $req
  if (-not $raw) {
    $why = if ($script:McpStderr) { "no response from the MCP binary: $($script:McpStderr.Trim())" } else { "no response from the MCP binary" }
    Check "mcp call $tool" $false $why
    return $null
  }
  $rpc = $null
  try { $rpc = $raw | ConvertFrom-Json } catch { $rpc = $null }
  if ($null -eq $rpc -or $rpc.error -or -not $rpc.result -or -not $rpc.result.content -or $rpc.result.content.Count -eq 0 -or -not $rpc.result.content[0].text) {
    $why = if ($rpc -and $rpc.error) { $rpc.error.message } else { "malformed JSON-RPC response" }
    Check "mcp call $tool" $false $why
    return $null
  }
  try { return ($rpc.result.content[0].text | ConvertFrom-Json) }
  catch {
    Check "mcp call $tool" $false "tool payload is not JSON: $($_.Exception.Message)"
    return $null
  }
}

function Set-RoutingMode([string]$mode) {
  $inFile = [System.IO.Path]::GetTempFileName()
  $outFile = [System.IO.Path]::GetTempFileName()
  $errFile = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -LiteralPath $inFile -Value ""
    return (Invoke-ExeBounded -ExeArgs @("--set-routing-mode", $mode, "--repo-root", "`"$Root`"") -InFile $inFile -OutFile $outFile -ErrFile $errFile -TimeoutMs 60000)
  } finally {
    Remove-Item -LiteralPath $inFile, $outFile, $errFile -Force -ErrorAction SilentlyContinue
  }
}

# 1. pages
# Probe once so an absent server fails fast instead of paying the per-request
# timeout on every page/module assertion.
try {
  $null = Invoke-WebRequest "$BaseUrl/fly-body" -UseBasicParsing -TimeoutSec 5
  Check "demo server reachable" $true $BaseUrl
} catch {
  Check "demo server reachable" $false "$BaseUrl is not serving ($($_.Exception.Message)); start the demo server or pass -BaseUrl"
  exit 1
}
$pages = @("fly-body", "brain-demo", "mb-3d", "vfb-brain", "fly-viz")
foreach ($p in $pages) {
  try {
    $code = (Invoke-WebRequest "$BaseUrl/$p" -UseBasicParsing -TimeoutSec 20).StatusCode
    Check "page /$p" ($code -eq 200) "$code"
  } catch { Check "page /$p" $false $_.Exception.Message }
}
foreach ($m in @("fly-app.js", "fly-anatomy.js", "fly-brain.js", "fly-state.js")) {
  try {
    $code = (Invoke-WebRequest "$BaseUrl/fly-static/$m" -UseBasicParsing -TimeoutSec 20).StatusCode
    Check "module $m" ($code -eq 200) "$code"
  } catch { Check "module $m" $false $_.Exception.Message }
}

# 2. routing mode normal
Check "set routing mode normal" (Set-RoutingMode "normal") ""
$ctx = Call "1" "system.context" "{}"
if ($ctx) { Check "system.context" ($ctx.status -eq "ok") ("mode=" + $ctx.data.routing_mode) }

# 3. fly mode + recommendation
# Always restore normal mode, even on a terminating error or early exit.
try {
Check "set routing mode fly" (Set-RoutingMode "fly") ""
$ctx = Call "2" "system.context" "{}"
if ($ctx) { Check "mode switch to fly" ($ctx.data.routing_mode -eq "fly") ("version=" + $ctx.data.routing_config_version) }

$rec = Call "3" "routing.fly_recommend" '{"task":"find anomalies in sensor telemetry","input_metadata":{"types":["csv"],"has_timestamp":true}}'
# Guard the derived checks so one failed Call yields one failure, not a cascade
# of misleading $null property accesses.
if ($rec) {
  Check "fly_recommend status" ($rec.status -eq "ok") ("route=" + $rec.data.route)
  Check "fly_recommend encoder" (("" + $rec.metrics.encoder) -match "mb") $rec.metrics.encoder
  Check "fly_recommend top-k" (($rec.data.recommendations | Measure-Object).Count -ge 3) (($rec.data.recommendations | Measure-Object).Count.ToString() + " recs")
}

# 4. dopamine counters before
$brainPath = Join-Path $Root "data\index\fly-brain.json"
$pamBefore = 0; $ppl1Before = 0
if (Test-Path $brainPath) {
  $brain = Get-Content $brainPath -Raw | ConvertFrom-Json
  $pamBefore = [int]$brain.dopamine.pam; $ppl1Before = [int]$brain.dopamine.ppl1
}
Check "trained brain artifact" (Test-Path $brainPath) ("pam=" + $pamBefore + " ppl1=" + $ppl1Before)

# 5. reward -> PAM
$fb = Call "4" "routing.fly_feedback" '{"task":"find anomalies in sensor telemetry","chosen":["timeseries.analyze","timeseries.detect_anomalies","charts.create"],"source":"accepted"}'
if ($fb) { Check "feedback accepted" ($fb.status -eq "ok") ("stored=" + $fb.data.stored) }
$pamAfter = $pamBefore
if (Test-Path $brainPath) { $pamAfter = [int]((Get-Content $brainPath -Raw | ConvertFrom-Json).dopamine.pam) }
Check "PAM incremented (reward)" ($pamAfter -gt $pamBefore) ("before=" + $pamBefore + " after=" + $pamAfter)

# 6. correction -> PPL1
$fb2 = Call "5" "routing.fly_feedback" '{"task":"classify suspicious login logs","chosen":["classification.classify","data.inspect"],"recommended":["vision.analyze"],"source":"codex-correction"}'
if ($fb2) { Check "feedback correction" ($fb2.status -eq "ok") ("stored=" + $fb2.data.stored) }
$ppl1After = $ppl1Before
if (Test-Path $brainPath) { $ppl1After = [int]((Get-Content $brainPath -Raw | ConvertFrom-Json).dopamine.ppl1) }
Check "PPL1 incremented (pain)" ($ppl1After -gt $ppl1Before) ("before=" + $ppl1Before + " after=" + $ppl1After)

# 7. neuron events log
$logPath = Join-Path $Root "logs\fly-neuron-events.jsonl"
$taskMarker = "find anomalies in sensor telemetry"
$events = @()
if (Test-Path $logPath) {
  # Read well beyond the two events under test and filter the recommend events
  # by the task marker so unrelated/earlier runs cannot push them out.
  $events = @(Get-Content $logPath -Tail 200 |
    ForEach-Object { try { $_ | ConvertFrom-Json } catch { $null } } |
    Where-Object { $_ })
}
$recEvents = @($events | Where-Object { $_.type -eq "recommend" -and $_.task -eq $taskMarker })
$fbEvents = @($events | Where-Object { $_.type -eq "feedback" })
Check "neuron events: recommend" ($recEvents.Count -ge 1) (($recEvents | Select-Object -Last 1).encoder)
$lastRec = $recEvents | Select-Object -Last 1
Check "active KC exported" ($lastRec -and ($lastRec.active_kc | Measure-Object).Count -gt 0) (($lastRec.active_kc | Measure-Object).Count.ToString() + " kc")
Check "neuron events: feedback" ($fbEvents.Count -ge 1) (($fbEvents | Select-Object -Last 1).source)

# 8. memory grows
$memPath = Join-Path $Root "data\index\fly-memory.json"
$memCount = 0
if (Test-Path $memPath) { $memCount = ((Get-Content $memPath -Raw | ConvertFrom-Json).examples | Measure-Object).Count }
Check "kNN memory" ($memCount -ge 2) ($memCount.ToString() + " examples")

# 9. restore normal mode
Check "set routing mode normal (restore)" (Set-RoutingMode "normal") ""
$ctx = Call "6" "system.context" "{}"
if ($ctx) { Check "restored normal mode" ($ctx.data.routing_mode -eq "normal") ("version=" + $ctx.data.routing_config_version) }
} finally {
  # Surface a failed restore as a FAIL instead of silently leaving fly mode.
  if (-not (Set-RoutingMode "normal")) {
    Check "restore normal mode (cleanup)" $false "the cleanup --set-routing-mode normal call failed"
  }
}

Write-Host ""
if ($fail -eq 0) { Write-Host "[e2e-fly] ALL CHECKS PASSED"; exit 0 }
Write-Host "[e2e-fly] $fail check(s) FAILED"; exit 1
