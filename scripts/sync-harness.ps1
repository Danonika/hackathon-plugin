param([ValidateSet("core", "full")][string]$InstallProfile)
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

function Get-TreeManifest([string]$Path) {
  $Path = (Get-Item -LiteralPath $Path).FullName.TrimEnd('\', '/')
  @(Get-ChildItem -LiteralPath $Path -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($Path.Length).TrimStart('\', '/')
      "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
    } | Sort-Object)
}

if (-not $PSCommandPath -or $MyInvocation.InvocationName -eq '.') {
  Fail "[sync-harness] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not (Test-Path -LiteralPath (Join-Path $Root "docker-compose.yml"))) {
  Fail "[sync-harness] refusing: '$Root' does not look like the hackathonkit repository root"
}

. (Join-Path $PSScriptRoot "skill-profile.ps1")
if (-not $InstallProfile) { $InstallProfile = Get-InstalledSkillProfile $root }
$global:LASTEXITCODE = 0
& (Join-Path $PSScriptRoot "sync-skills.ps1") -InstallProfile $InstallProfile
if ($LASTEXITCODE -ne 0) {
  Fail "[sync-harness] sync-skills failed with exit code $LASTEXITCODE"
}

$SkillsSrc = Join-Path $Root "plugins/hackathonkit/skills"
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

# Claude Code reads project skills from .claude/skills. Build the mirror in a
# temp directory and swap it in under a lock so a failure cannot leave it
# missing/partial and concurrent runs cannot interleave the two renames.
$ClaudeSkills = Join-Path $Root ".claude/skills"
. (Join-Path $PSScriptRoot "file-lock.ps1")
$lockHandle = Enter-MirrorLock $Root "claude-skills.lock"
$ClaudeTmp = "$ClaudeSkills.tmp.$PID"
$ClaudeOld = $null
try {
  if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force }
  New-Item -ItemType Directory -Force -Path $ClaudeTmp | Out-Null
  Copy-NoReparse $SkillsSrc $ClaudeTmp
  # Compare the full relative file set (path + hash), not just top-level counts.
  $srcManifest = Get-TreeManifest $SkillsSrc
  $dstManifest = Get-TreeManifest $ClaudeTmp
  if (Compare-Object -ReferenceObject $srcManifest -DifferenceObject $dstManifest) {
    Fail "[sync-harness] claude mirror mismatch: source and copy differ"
  }
  Select-StagedSkillProfile $ClaudeTmp $InstallProfile
  # Move the live mirror aside before the swap (two renames, not atomic).
  $ClaudeOld = "$ClaudeSkills.previous"
  if ([IO.Directory]::Exists($ClaudeOld)) {
    if (-not [IO.Directory]::Exists($ClaudeSkills)) { [IO.Directory]::Move($ClaudeOld,$ClaudeSkills) }
    else { Remove-Item -LiteralPath $ClaudeOld -Recurse -Force }
  }
  if (Test-Path -LiteralPath $ClaudeSkills) { Move-Item -LiteralPath $ClaudeSkills -Destination $ClaudeOld -Force }
  try {
    Move-Item -LiteralPath $ClaudeTmp -Destination $ClaudeSkills -Force
  } catch {
    if (Test-Path -LiteralPath $ClaudeOld) { Move-Item -LiteralPath $ClaudeOld -Destination $ClaudeSkills -Force }
    throw
  }
  # Mirror the plugin commands into the harness project command directories.
  $CmdsSrc = Join-Path $Root "plugins/hackathonkit/commands"
  if (-not (Test-Path -LiteralPath $CmdsSrc)) {
    Fail "[sync-harness] commands source not found: $CmdsSrc"
  }
  $commands = @(Get-ChildItem -LiteralPath $CmdsSrc -File -Filter *.md | Where-Object {
    -not ($_.Attributes -band [IO.FileAttributes]::ReparsePoint)
  })
  if ($commands.Count -eq 0) { throw "[sync-harness] no canonical Markdown commands in $CmdsSrc" }
  foreach ($dest in @((Join-Path $Root ".opencode/commands"), (Join-Path $Root ".claude/commands"))) {
    $parent = [IO.Directory]::CreateDirectory((Split-Path -Parent $dest))
    if ($parent.Attributes -band [IO.FileAttributes]::ReparsePoint) {
      throw "[sync-harness] refusing linked mirror parent $parent"
    }
    $previous = "$dest.previous"
    if ([IO.Directory]::Exists($previous)) {
      if (-not [IO.Directory]::Exists($dest)) { [IO.Directory]::Move($previous, $dest) }
      else { Remove-Item -LiteralPath $previous -Recurse -Force }
    }
    $stage = "$dest.tmp.$([Guid]::NewGuid().ToString('N'))"
    try {
      [void][IO.Directory]::CreateDirectory($stage)
      foreach ($file in $commands) {
        $copy = Join-Path $stage $file.Name
        [IO.File]::Copy($file.FullName, $copy)
        if ((Get-FileHash -LiteralPath $copy).Hash -ne (Get-FileHash -LiteralPath $file.FullName).Hash) {
          throw "[sync-harness] command copy mismatch: $($file.Name)"
        }
      }
      if ([IO.Directory]::Exists($dest)) { [IO.Directory]::Move($dest, $previous) }
      try { [IO.Directory]::Move($stage, $dest) }
      catch {
        if ([IO.Directory]::Exists($previous) -and -not [IO.Directory]::Exists($dest)) {
          [IO.Directory]::Move($previous, $dest)
        }
        throw
      }
      if ([IO.Directory]::Exists($previous)) { Remove-Item -LiteralPath $previous -Recurse -Force }
    } finally {
      if ([IO.Directory]::Exists($stage)) { Remove-Item -LiteralPath $stage -Recurse -Force }
    }
  }

  $Required = @(
    "AGENTS.md",
    ".codex/AGENTS.md",
    ".codex/config.toml",
    ".codex/agents/researcher.toml",
    ".codex/agents/reviewer.toml",
    ".codex/agents/evaluator.toml",
    ".claude-plugin/plugin.json",
    ".claude-plugin/marketplace.json",
    ".opencode/opencode.json",
    ".cursor/rules/hackathonkit.mdc",
    "plugins/hackathonkit/hooks/codex-hooks.json",
    "plugins/hackathonkit/commands/plan.md",
    "plugins/hackathonkit/commands/research.md",
    "plugins/hackathonkit/commands/build.md",
    "plugins/hackathonkit/commands/evaluate.md",
    "plugins/hackathonkit/commands/verify.md"
  )
  $Missing = 0
  foreach ($f in $Required) {
    if (-not (Test-Path -LiteralPath (Join-Path $Root $f))) {
      Write-Warning "[sync-harness] missing adapter file: $f"
      $Missing++
    }
  }

  # Parity with sync-harness.sh: validate the committed JSON manifests. Keep a
  # separate counter so missing and invalid files are reported distinctly.
  $Invalid = 0
  foreach ($f in @(".claude-plugin/plugin.json", ".claude-plugin/marketplace.json", ".opencode/opencode.json", "plugins/hackathonkit/hooks/codex-hooks.json")) {
    $path = Join-Path $Root $f
    if (-not (Test-Path -LiteralPath $path)) { continue }
    try {
      $null = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    } catch {
      Write-Warning "[sync-harness] invalid JSON manifest: $f ($($_.Exception.Message))"
      $Invalid++
    }
  }
  if ($Missing -ne 0 -or $Invalid -ne 0) {
    Write-Warning "[sync-harness] adapters incomplete: $Missing missing, $Invalid invalid"
    exit 1
  }

  $Count = (Get-ChildItem -LiteralPath $ClaudeSkills -Directory | Measure-Object).Count
  Write-Host "[sync-harness] adapters ready: codex, opencode, claude, cursor ($Count skills mirrored to .claude/skills)"
} finally {
  if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force -ErrorAction SilentlyContinue }
  # Single owner of backup deletion: keep it only if the live mirror is missing
  # (a failed restore), since it is then the only copy and must survive.
  if ($ClaudeOld -and (Test-Path -LiteralPath $ClaudeOld) -and (Test-Path -LiteralPath $ClaudeSkills)) {
    Remove-Item -LiteralPath $ClaudeOld -Recurse -Force -ErrorAction SilentlyContinue
  }
  if ($ClaudeOld -and (Test-Path -LiteralPath $ClaudeOld) -and -not (Test-Path -LiteralPath $ClaudeSkills)) {
    Move-Item -LiteralPath $ClaudeOld -Destination $ClaudeSkills -ErrorAction Continue
  }
  $lockHandle.Dispose()
}
exit 0
