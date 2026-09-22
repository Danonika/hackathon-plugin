$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

function Copy-NoReparse([string]$From, [string]$To) {
  New-Item -ItemType Directory -Force -Path $To | Out-Null
  Get-ChildItem -LiteralPath $From -Force | ForEach-Object {
    if ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { return }
    $dest = Join-Path $To $_.Name
    if ($_.PSIsContainer) { Copy-NoReparse $_.FullName $dest }
    else { Copy-Item -LiteralPath $_.FullName -Destination $dest -Force }
  }
}

if (-not $PSCommandPath) {
  Fail "[sync-harness] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not (Test-Path -LiteralPath (Join-Path $Root "docker-compose.yml"))) {
  Fail "[sync-harness] refusing: '$Root' does not look like the hackathonkit repository root"
}

$global:LASTEXITCODE = 0
& (Join-Path $PSScriptRoot "sync-skills.ps1")
if ($LASTEXITCODE -ne 0) {
  Fail "[sync-harness] sync-skills failed with exit code $LASTEXITCODE"
}

$SkillsSrc = Join-Path $Root "plugins\hackathonkit\skills"
if (-not (Test-Path -LiteralPath $SkillsSrc)) {
  Fail "[sync-harness] skills source not found: $SkillsSrc"
}
if ((Get-ChildItem -LiteralPath $SkillsSrc -Directory | Measure-Object).Count -eq 0) {
  Fail "[sync-harness] skills source is empty: $SkillsSrc"
}
$missingPayload = @(Get-ChildItem -LiteralPath $SkillsSrc -Directory | Where-Object {
  -not (Test-Path -LiteralPath (Join-Path $_.FullName "SKILL.md"))
})
if ($missingPayload.Count -gt 0) {
  Fail "[sync-harness] skill directories without SKILL.md: $($missingPayload.Name -join ', ')"
}

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
$ClaudeSkills = Join-Path $Root ".claude\skills"
$ClaudeTmp = "$ClaudeSkills.tmp.$PID"
if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $ClaudeTmp | Out-Null
try {
  Copy-NoReparse $SkillsSrc $ClaudeTmp
  $srcSkillCount = (Get-ChildItem -LiteralPath $SkillsSrc -Directory).Count
  $dstSkillCount = (Get-ChildItem -LiteralPath $ClaudeTmp -Directory).Count
  if ($srcSkillCount -ne $dstSkillCount) {
    Fail "[sync-harness] claude mirror mismatch: $srcSkillCount source, $dstSkillCount copied"
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
  if ($ClaudeOld -and (Test-Path -LiteralPath $ClaudeOld)) { Remove-Item -LiteralPath $ClaudeOld -Recurse -Force -ErrorAction SilentlyContinue }
}

# Mirror the plugin commands into the harness project command directories.
$CmdsSrc = Join-Path $Root "plugins\hackathonkit\commands"
if (-not (Test-Path -LiteralPath $CmdsSrc)) {
  Fail "[sync-harness] commands source not found: $CmdsSrc"
}
foreach ($dest in @((Join-Path $Root ".opencode\commands"), (Join-Path $Root ".claude\commands"))) {
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Get-ChildItem -LiteralPath $CmdsSrc -Filter *.md | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $dest $_.Name) -Force
  }
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
  "plugins\hackathonkit\hooks\codex-hooks.json",
  "plugins\hackathonkit\commands\verify.md",
  ".codex\prompts\verify.md"
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
    Write-Warning "[sync-harness] invalid JSON manifest: $f ($($_.Exception.Message))"
    $Missing++
  }
}
if ($Missing -ne 0) { exit 1 }

$Count = (Get-ChildItem -Directory $ClaudeSkills | Measure-Object).Count
Write-Host "[sync-harness] adapters ready: codex, opencode, claude, cursor ($Count skills mirrored to .claude/skills)"
exit 0
