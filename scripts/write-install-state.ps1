param(
  [Parameter(Mandatory = $true)][string]$Root,
  [string]$Profile = "full",
  [string]$Runtime = "unknown"
)

$pluginJson = Join-Path $Root "plugins\hackathonkit\.codex-plugin\plugin.json"
$Version = "unknown"
if (Test-Path $pluginJson) {
  $match = Select-String -Path $pluginJson -Pattern '"version"\s*:\s*"([^"]+)"' | Select-Object -First 1
  if ($match) { $Version = $match.Matches[0].Groups[1].Value }
}

$Skills = 0
$mirror = Join-Path $Root ".agents\skills"
if (Test-Path $mirror) {
  $Skills = (Get-ChildItem -Directory $mirror | Measure-Object).Count
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
$out = Join-Path $Root "configs\install-state.json"
$state | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $out -Encoding UTF8
Write-Host "[install-state] wrote configs/install-state.json (version $Version, profile $Profile, runtime $Runtime)"
