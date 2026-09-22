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

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
$ClaudeSkills = Join-Path $Root ".claude\skills"
$ClaudeTmp = "$ClaudeSkills.tmp.$PID"
if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $ClaudeTmp | Out-Null
try {
  Get-ChildItem -LiteralPath (Join-Path $Root "plugins\hackathonkit\skills") -Force |
    Copy-Item -Destination $ClaudeTmp -Recurse -Force
  if (Test-Path -LiteralPath $ClaudeSkills) { Remove-Item -LiteralPath $ClaudeSkills -Recurse -Force }
  Move-Item -LiteralPath $ClaudeTmp -Destination $ClaudeSkills -Force
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
    Write-Error "[sync-harness] missing adapter file: $f"
    $Missing++
  }
}
if ($Missing -ne 0) { exit 1 }

$Count = (Get-ChildItem -Directory $ClaudeSkills | Measure-Object).Count
Write-Host "[sync-harness] adapters ready: codex, opencode, claude, cursor ($Count skills mirrored to .claude/skills)"
