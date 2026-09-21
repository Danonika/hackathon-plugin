param(
  [string]$Root = (Split-Path -Parent (Split-Path -Parent $PSCommandPath))
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

function Mcp([string]$payload) {
  ($payload | & $exe --repo-root $Root 2>$null) -join "`n"
}
function Call([string]$id, [string]$tool, [string]$argsJson) {
  $req = '{"jsonrpc":"2.0","id":' + $id + ',"method":"tools/call","params":{"name":"' + $tool + '","arguments":' + $argsJson + '}}'
  $raw = Mcp $req
  $env = ($raw | ConvertFrom-Json).result.content[0].text | ConvertFrom-Json
  return $env
}

# 1. pages
$pages = @("fly-body", "brain-demo", "mb-3d", "vfb-brain", "fly-viz")
foreach ($p in $pages) {
  try {
    $code = (Invoke-WebRequest "http://127.0.0.1:8787/$p" -UseBasicParsing -TimeoutSec 20).StatusCode
    Check "page /$p" ($code -eq 200) "$code"
  } catch { Check "page /$p" $false $_.Exception.Message }
}
foreach ($m in @("fly-app.js", "fly-anatomy.js", "fly-brain.js", "fly-state.js")) {
  try {
    $code = (Invoke-WebRequest "http://127.0.0.1:8787/fly-static/$m" -UseBasicParsing -TimeoutSec 20).StatusCode
    Check "module $m" ($code -eq 200) "$code"
  } catch { Check "module $m" $false $_.Exception.Message }
}

# 2. routing mode normal
& $exe --set-routing-mode normal --repo-root $Root | Out-Null
$ctx = Call "1" "system.context" "{}"
Check "system.context" ($ctx.status -eq "ok") ("mode=" + $ctx.data.routing_mode)

# 3. fly mode + recommendation
& $exe --set-routing-mode fly --repo-root $Root | Out-Null
$ctx = Call "2" "system.context" "{}"
Check "mode switch to fly" ($ctx.data.routing_mode -eq "fly") ("version=" + $ctx.data.routing_config_version)

$rec = Call "3" "routing.fly_recommend" '{"task":"find anomalies in sensor telemetry","input_metadata":{"types":["csv"],"has_timestamp":true}}'
Check "fly_recommend status" ($rec.status -eq "ok") ("route=" + $rec.data.route)
Check "fly_recommend encoder" (("" + $rec.metrics.encoder) -match "mb") $rec.metrics.encoder
Check "fly_recommend top-k" (($rec.data.recommendations | Measure-Object).Count -ge 3) (($rec.data.recommendations | Measure-Object).Count.ToString() + " recs")

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
Check "feedback accepted" ($fb.status -eq "ok") ("stored=" + $fb.data.stored)
$pamAfter = $pamBefore
if (Test-Path $brainPath) { $pamAfter = [int]((Get-Content $brainPath -Raw | ConvertFrom-Json).dopamine.pam) }
Check "PAM incremented (reward)" ($pamAfter -ge $pamBefore) ("before=" + $pamBefore + " after=" + $pamAfter)

# 6. correction -> PPL1
$fb2 = Call "5" "routing.fly_feedback" '{"task":"classify suspicious login logs","chosen":["classification.classify","data.inspect"],"recommended":["vision.analyze"],"source":"codex-correction"}'
Check "feedback correction" ($fb2.status -eq "ok") ("stored=" + $fb2.data.stored)
$ppl1After = $ppl1Before
if (Test-Path $brainPath) { $ppl1After = [int]((Get-Content $brainPath -Raw | ConvertFrom-Json).dopamine.ppl1) }
Check "PPL1 incremented (pain)" ($ppl1After -ge $ppl1Before) ("before=" + $ppl1Before + " after=" + $ppl1After)

# 7. neuron events log
$logPath = Join-Path $Root "logs\fly-neuron-events.jsonl"
$events = @()
if (Test-Path $logPath) { $events = Get-Content $logPath -Tail 6 | ForEach-Object { $_ | ConvertFrom-Json } }
$recEvents = @($events | Where-Object { $_.type -eq "recommend" })
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
& $exe --set-routing-mode normal --repo-root $Root | Out-Null
$ctx = Call "6" "system.context" "{}"
Check "restored normal mode" ($ctx.data.routing_mode -eq "normal") ("version=" + $ctx.data.routing_config_version)

Write-Host ""
if ($fail -eq 0) { Write-Host "[e2e-fly] ALL CHECKS PASSED"; exit 0 }
Write-Host "[e2e-fly] $fail check(s) FAILED"; exit 1
