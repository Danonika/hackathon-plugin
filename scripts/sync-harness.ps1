$ErrorActionPreference = "Stop"
if (-not $PSCommandPath) {
  Write-Error "[sync-harness] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not (Test-Path -LiteralPath (Join-Path $Root "docker-compose.yml"))) {
  Write-Error "[sync-harness] refusing: '$Root' does not look like the hackathonkit repository root"
  exit 1
}

$global:LASTEXITCODE = 0
& (Join-Path $PSScriptRoot "sync-skills.ps1")
if ($LASTEXITCODE -ne 0) {
  Write-Error "[sync-harness] sync-skills failed with exit code $LASTEXITCODE"
  exit 1
}

$SkillsSrc = Join-Path $Root "plugins\hackathonkit\skills"
if (-not (Test-Path -LiteralPath $SkillsSrc)) {
  Write-Error "[sync-harness] skills source not found: $SkillsSrc"
  exit 1
}
if ((Get-ChildItem -LiteralPath $SkillsSrc -Directory | Measure-Object).Count -eq 0) {
  Write-Error "[sync-harness] skills source is empty: $SkillsSrc"
  exit 1
}

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
$ClaudeSkills = Join-Path $Root ".claude\skills"
$ClaudeTmp = "$ClaudeSkills.tmp.$PID"
if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $ClaudeTmp | Out-Null
try {
  Get-ChildItem -LiteralPath $SkillsSrc -Force |
    Copy-Item -Destination $ClaudeTmp -Recurse -Force
  $srcSkillCount = (Get-ChildItem -LiteralPath $SkillsSrc -Directory).Count
  $dstSkillCount = (Get-ChildItem -LiteralPath $ClaudeTmp -Directory).Count
  if ($srcSkillCount -ne $dstSkillCount) {
    Write-Error "[sync-harness] claude mirror mismatch: $srcSkillCount source, $dstSkillCount copied"
    exit 1
  }
  # Move the live mirror aside before the swap so a failure cannot leave it
  # missing or half-populated.
  $ClaudeOld = "$ClaudeSkills.old.$PID"
  if (Test-Path -LiteralPath $ClaudeSkills) { Move-Item -LiteralPath $ClaudeSkills -Destination $ClaudeOld -Force }
  try {
    Move-Item -LiteralPath $ClaudeTmp -Destination $ClaudeSkills -Force
  } catch {
    if (Test-Path -LiteralPath $ClaudeOld) { Move-Item -LiteralPath $ClaudeOld -Destination $ClaudeSkills -Force }
    throw
  }
  if (Test-Path -LiteralPath $ClaudeOld) { Remove-Item -LiteralPath $ClaudeOld -Recurse -Force }
} finally {
  if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force -ErrorAction SilentlyContinue }
}

$Required = @(
  "AGENTS.md",
  ".codex\AGENTS.md",
  ".codex\config.toml",
  ".codex\agents\researcher.toml",
  ".codex\agents\reviewer.toml",
  ".codex\agents\evaluator.toml",
  ".claude-plugin\plugin.json",
  ".claude-plugin\marketplace.json",
  ".opencode\opencode.json",
  ".cursor\rules\hackathonkit.mdc",
  "plugins\hackathonkit\hooks\codex-hooks.json"
)
$Missing = 0
foreach ($f in $Required) {
  if (-not (Test-Path (Join-Path $Root $f))) {
    Write-Warning "[sync-harness] missing adapter file: $f"
    $Missing++
  }
}
if ($Missing -ne 0) { exit 1 }

# Parity with sync-harness.sh: validate the committed JSON manifests.
foreach ($f in @(".claude-plugin\plugin.json", ".claude-plugin\marketplace.json", ".opencode\opencode.json", "plugins\hackathonkit\hooks\codex-hooks.json")) {
  $path = Join-Path $Root $f
  try {
    $null = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
  } catch {
    Write-Error "[sync-harness] invalid JSON manifest: $f ($($_.Exception.Message))"
    $Missing++
  }
}
if ($Missing -ne 0) { exit 1 }

$Count = (Get-ChildItem -Directory $ClaudeSkills | Measure-Object).Count
Write-Host "[sync-harness] adapters ready: codex, opencode, claude, cursor ($Count skills mirrored to .claude/skills)"
