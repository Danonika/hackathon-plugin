param(
  [Parameter(Mandatory = $true)][string]$Root,
  [string]$Profile = "full",
  [string]$Runtime = "unknown"
)

if ($Profile -notin @("core", "full")) {
  Write-Error "[install-state] invalid profile: $Profile (expected core or full)"
  exit 1
}
if ($Runtime -notin @("binary", "docker", "unknown")) {
  Write-Error "[install-state] invalid runtime: $Runtime (expected binary, docker or unknown)"
  exit 1
}

$pluginJson = Join-Path $Root "plugins\hackathonkit\.codex-plugin\plugin.json"
$Version = "unknown"
if (Test-Path $pluginJson) {
  $match = Select-String -Path $pluginJson -Pattern '"version"\s*:\s*"([^"]+)"' | Select-Object -First 1
  if ($match) { $Version = $match.Matches[0].Groups[1].Value }
}

$Skills = 0
$mirror = Join-Path $Root ".agents\skills"
if (Test-Path $mirror) {
  $srcSkills = Join-Path $Root "plugins\hackathonkit\skills"
  $srcNames = @(Get-ChildItem -LiteralPath $srcSkills -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name | Sort-Object)
  $dstNames = @(Get-ChildItem -LiteralPath $mirror -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name | Sort-Object)
  $missing = @($srcNames | Where-Object { $_ -notin $dstNames })
  if ($missing.Count -gt 0) {
    Write-Warning "[install-state] mirror is missing $($missing.Count) skill(s): $($missing -join ', ')"
  }
  $Skills = $dstNames.Count
}

$state = [ordered]@{
  version      = $Version
  profile      = $Profile
  runtime      = $Runtime
  skills       = $Skills
  components   = [ordered]@{
    hooks    = $true
    agents   = $true
    learning = $true
    adapters = @("codex", "opencode", "claude", "cursor")
  }
  generated_at = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
$configsDir = Join-Path $Root "configs"
New-Item -ItemType Directory -Force -Path $configsDir | Out-Null
$out = Join-Path $configsDir "install-state.json"
try {
  $state | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $out -Encoding UTF8 -ErrorAction Stop
} catch {
  Write-Error "[install-state] cannot write ${out}: $($_.Exception.Message)"
  exit 1
}
Write-Host "[install-state] wrote configs/install-state.json (version $Version, profile $Profile, runtime $Runtime)"
