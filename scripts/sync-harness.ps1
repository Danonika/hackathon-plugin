$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

& (Join-Path $PSScriptRoot "sync-skills.ps1")

# Claude Code reads project skills from .claude/skills.
$ClaudeSkills = Join-Path $Root ".claude\skills"
if (Test-Path $ClaudeSkills) { Remove-Item -Recurse -Force $ClaudeSkills }
New-Item -ItemType Directory -Force -Path $ClaudeSkills | Out-Null
Copy-Item -Recurse -Force (Join-Path $Root "plugins\hackathonkit\skills\*") $ClaudeSkills

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
