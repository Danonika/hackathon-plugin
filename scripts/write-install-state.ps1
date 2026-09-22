param(
  [Parameter(Mandatory = $true)][string]$Root,
  [string]$Profile = "full",
  [string]$Runtime = "unknown"
)

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

if (-not $Root -or -not (Test-Path -LiteralPath $Root -PathType Container)) {
  Fail "[install-state] root directory not found: $Root"
}
if (-not (Test-Path -LiteralPath (Join-Path $Root "docker-compose.yml"))) {
  Fail "[install-state] refusing: '$Root' does not look like the hackathonkit repository root"
}
if ($Profile -notin @("core", "full")) {
  Fail "[install-state] invalid profile: $Profile (expected core or full)"
}
if ($Runtime -notin @("binary", "docker", "unknown")) {
  Fail "[install-state] invalid runtime: $Runtime (expected binary, docker or unknown)"
}

$pluginJson = Join-Path $Root "plugins\hackathonkit\.codex-plugin\plugin.json"
$Version = "unknown"
if (Test-Path $pluginJson) {
  try {
    $plugin = Get-Content -LiteralPath $pluginJson -Raw | ConvertFrom-Json
    if ($plugin.version) { $Version = [string]$plugin.version }
  } catch {
    Write-Warning "[install-state] cannot parse $pluginJson; version stays unknown"
  }
}

$Skills = 0
$mirror = Join-Path $Root ".agents\skills"
$srcSkills = Join-Path $Root "plugins\hackathonkit\skills"
if (-not (Test-Path -LiteralPath $srcSkills)) {
  Write-Warning "[install-state] skills source missing: $srcSkills"
}
if (Test-Path $mirror) {
  $srcNames = @(Get-ChildItem -LiteralPath $srcSkills -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name | Sort-Object)
  $dstNames = @(Get-ChildItem -LiteralPath $mirror -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name | Sort-Object)
  $missing = @($srcNames | Where-Object { $_ -notin $dstNames })
  if ($missing.Count -gt 0) {
    Write-Warning "[install-state] mirror is missing $($missing.Count) skill(s): $($missing -join ', ')"
  }
  $Skills = $dstNames.Count
}

# Derive components from what is actually present instead of assuming.
$hooksInstalled = Test-Path -LiteralPath (Join-Path $Root "plugins\hackathonkit\hooks\codex-hooks.json")
$agentsDir = Join-Path $Root ".codex\agents"
$agentsInstalled = (Test-Path -LiteralPath $agentsDir) -and
  ((Get-ChildItem -LiteralPath $agentsDir -Filter *.toml -ErrorAction SilentlyContinue | Measure-Object).Count -gt 0)
$learningInstalled = Test-Path -LiteralPath (Join-Path $Root "plugins\hackathonkit\skills\learning-loop")
$adapterCandidates = [ordered]@{
  codex    = "AGENTS.md"
  opencode = ".opencode\opencode.json"
  claude   = ".claude-plugin\plugin.json"
  cursor   = ".cursor\rules\hackathonkit.mdc"
}
$adapters = @($adapterCandidates.Keys | Where-Object {
  Test-Path -LiteralPath (Join-Path $Root $adapterCandidates[$_])
})

$state = [ordered]@{
  version      = $Version
  profile      = $Profile
  runtime      = $Runtime
  skills       = $Skills
  components   = [ordered]@{
    hooks    = $hooksInstalled
    agents   = $agentsInstalled
    learning = $learningInstalled
    adapters = $adapters
  }
  generated_at = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
$configsDir = Join-Path $Root "configs"
New-Item -ItemType Directory -Force -Path $configsDir | Out-Null
$out = Join-Path $configsDir "install-state.json"
try {
  $state | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $out -Encoding UTF8 -ErrorAction Stop
} catch {
  Fail "[install-state] cannot write ${out}: $($_.Exception.Message)"
}
Write-Host "[install-state] wrote configs/install-state.json (version $Version, profile $Profile, runtime $Runtime)"
exit 0
