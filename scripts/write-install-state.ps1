param(
  [Parameter(Mandatory = $true)][string]$Root,
  [Alias("Profile")][string]$InstallProfile = "full",
  [string]$Runtime = "unknown"
)

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

if (-not $Root -or -not (Test-Path -LiteralPath $Root -PathType Container)) {
  Fail "[install-state] root directory not found: $Root"
}
# Canonicalize FIRST, then re-validate the resolved root: a symlink swapped
# between validation and resolution cannot redirect the reads/writes below.
$Root = (Resolve-Path -LiteralPath $Root).Path
if (-not (Test-Path -LiteralPath (Join-Path $Root "docker-compose.yml")) -or -not (Test-Path -LiteralPath (Join-Path $Root "go.mod"))) {
  Fail "[install-state] refusing: '$Root' does not look like the hackathonkit repository root"
}
if ($InstallProfile -notin @("core", "full")) {
  Fail "[install-state] invalid profile: $InstallProfile (expected core or full)"
}
$InstallProfile = $InstallProfile.ToLowerInvariant()
if ($Runtime -notin @("binary", "docker", "unknown")) {
  Fail "[install-state] invalid runtime: $Runtime (expected binary, docker or unknown)"
}

$pluginJson = Join-Path $Root "plugins/hackathonkit/.codex-plugin/plugin.json"
$Version = "unknown"
if (Test-Path -LiteralPath $pluginJson) {
  try {
    $plugin = Get-Content -LiteralPath $pluginJson -Raw | ConvertFrom-Json
    if (-not $plugin.version) {
      Write-Warning "[install-state] $pluginJson has no version; recording 'unknown'"
    } elseif ($plugin.version -is [string]) {
      $Version = $plugin.version
    } else {
      Write-Warning "[install-state] plugin version is not a string; coercing to '$([string]$plugin.version)'"
      $Version = [string]$plugin.version
    }
  } catch {
    Write-Warning "[install-state] cannot parse $pluginJson; version stays unknown"
  }
} else {
  Write-Warning "[install-state] plugin manifest is missing: $pluginJson; version stays unknown"
}

$Skills = 0
$mirror = Join-Path $Root ".agents/skills"
. (Join-Path $PSScriptRoot "skill-profile.ps1")
$srcSkills = Join-Path $Root "plugins/hackathonkit/skills"
if (-not (Test-Path -LiteralPath $srcSkills)) {
  Fail "[install-state] skills source missing: $srcSkills"
}
if (-not (Test-Path -LiteralPath $mirror)) {
  Write-Warning "[install-state] skills mirror is missing: $mirror; skills count is 0"
}
if (Test-Path -LiteralPath $mirror) {
  $srcNames = @(Get-ChildItem -LiteralPath $srcSkills -Directory -ErrorAction SilentlyContinue | Where-Object { Test-SkillInProfile $_.FullName $InstallProfile } | Select-Object -ExpandProperty Name | Sort-Object)
  $dstNames = @(Get-ChildItem -LiteralPath $mirror -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name | Sort-Object)
  $missing = @($srcNames | Where-Object { $_ -notin $dstNames })
  if ($missing.Count -gt 0) {
    Write-Warning "[install-state] mirror is missing $($missing.Count) skill(s): $($missing -join ', ')"
  }
  $Skills = $dstNames.Count
}

# Component flags all use one definition: "present where the consumer reads it".
#   hooks    - the Codex hook config shipped with the plugin (what Codex loads)
#   agents   - the user's .codex/agents contains at least one role
#   learning - the learning-loop skill exists in the installed .agents mirror
#   adapters - the per-harness adapter files are present
$hooksInstalled = Test-Path -LiteralPath (Join-Path $Root "plugins/hackathonkit/hooks/codex-hooks.json")
$agentsDir = Join-Path $Root ".codex/agents"
$agentsInstalled = (Test-Path -LiteralPath $agentsDir) -and
  ((Get-ChildItem -LiteralPath $agentsDir -Filter *.toml -ErrorAction SilentlyContinue | Measure-Object).Count -gt 0)
$learningInstalled = Test-Path -LiteralPath (Join-Path $mirror "learning-loop")
$adapterCandidates = [ordered]@{
  codex    = "AGENTS.md"
  opencode = ".opencode/opencode.json"
  claude   = ".claude-plugin/plugin.json"
  cursor   = ".cursor/rules/hackathonkit.mdc"
}
$adapters = @($adapterCandidates.Keys | Where-Object {
  Test-Path -LiteralPath (Join-Path $Root $adapterCandidates[$_])
})

$state = [ordered]@{
  version      = $Version
  profile      = $InstallProfile
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
  $json = $state | ConvertTo-Json -Depth 4
  # Write to a sibling temp file with BOM-less UTF-8 and swap it in so an
  # interrupted run cannot truncate the previous state.
  $tmp = "$out.tmp.$PID"
  try {
    [System.IO.File]::WriteAllText($tmp, $json, (New-Object System.Text.UTF8Encoding($false)))
    if ([IO.File]::Exists($out)) { [IO.File]::Replace($tmp, $out, [System.Management.Automation.Language.NullString]::Value) }
    else {
      try { [IO.File]::Move($tmp, $out) }
      catch [IO.IOException] {
        if (-not [IO.File]::Exists($out)) { throw }
        [IO.File]::Replace($tmp, $out, [System.Management.Automation.Language.NullString]::Value)
      }
    }
  } finally {
    if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
  }
} catch {
  Fail "[install-state] cannot write ${out}: $($_.Exception.Message)"
}
if (-not (Test-Path -LiteralPath $out)) { Fail "[install-state] atomic write did not produce $out" }
Write-Host "[install-state] wrote configs/install-state.json (version $Version, profile $InstallProfile, runtime $Runtime)"
exit 0
