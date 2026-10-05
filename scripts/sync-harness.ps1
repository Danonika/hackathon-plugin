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
  @(Get-ChildItem -LiteralPath $Path -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($Path.Length).TrimStart('\', '/')
      "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
    } | Sort-Object)
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
$ClaudeLock = "$ClaudeSkills.lock"
$ClaudeTmp = "$ClaudeSkills.tmp.$PID"
$ClaudeOld = $null
if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $ClaudeTmp | Out-Null

$lockParent = Split-Path -Parent $ClaudeLock
if (-not (Test-Path -LiteralPath $lockParent)) { New-Item -ItemType Directory -Force -Path $lockParent | Out-Null }
$lockToken = "$PID-$([System.Guid]::NewGuid().ToString('N'))"
$locked = $false
for ($i = 0; $i -lt 100; $i++) {
  try {
    New-Item -ItemType Directory -Path $ClaudeLock -ErrorAction Stop | Out-Null
    try {
      # Fail hard (and roll back) if the metadata cannot be written: a lock
      # without a readable pid/owner can never be released by its own owner.
      Set-Content -LiteralPath (Join-Path $ClaudeLock "pid") -Value $PID -ErrorAction Stop
      Set-Content -LiteralPath (Join-Path $ClaudeLock "owner") -Value $lockToken -ErrorAction Stop
    } catch {
      Remove-Item -LiteralPath $ClaudeLock -Recurse -Force -ErrorAction SilentlyContinue
      throw
    }
    $locked = $true
    break
  } catch {
    if (Test-Path -LiteralPath $ClaudeLock) {
      # Evict only when the owning process is gone and the lock is old enough.
      $holder = $null
      $pidFile = Join-Path $ClaudeLock "pid"
      if (Test-Path -LiteralPath $pidFile) {
        try { $holder = Get-Content -LiteralPath $pidFile -Force -ErrorAction Stop | Select-Object -First 1 } catch { $holder = $null }
      }
      $stale = $false
      $holderPid = 0
      if ($holder -and [int]::TryParse(("$holder").Trim(), [ref]$holderPid) -and $holderPid -gt 0) {
        $holderProc = Get-Process -Id $holderPid -ErrorAction SilentlyContinue
        if (-not $holderProc) {
          try {
            $agePid = (Get-Date) - (Get-Item -LiteralPath $ClaudeLock -Force -ErrorAction Stop).LastWriteTime
            if ($agePid.TotalSeconds -gt 60) { $stale = $true }
          } catch { $stale = $true }
        } else {
          # PID existence alone is not proof of ownership: if the process
          # started after the lock was created, the PID was recycled by an
          # unrelated process and the lock is genuinely stale.
          try {
            $lockItem = Get-Item -LiteralPath $ClaudeLock -Force -ErrorAction Stop
            if ($holderProc.StartTime -gt $lockItem.LastWriteTime.AddSeconds(2)) { $stale = $true }
          } catch { }
        }
      } else {
        try {
          $age = (Get-Date) - (Get-Item -LiteralPath $ClaudeLock -Force -ErrorAction Stop).LastWriteTime
          if ($age.TotalSeconds -gt 60) { $stale = $true }
        } catch { $stale = $false }
      }
      if ($stale) {
        $staleLock = "$ClaudeLock.stale.$PID"
        # Snapshot the owner token, then re-read it after the move. If it changed
        # a peer re-created the lock in between, so restore the live lock instead
        # of deleting it.
        $ownerBefore = $null
        $ownerFile = Join-Path $ClaudeLock "owner"
        if (Test-Path -LiteralPath $ownerFile) {
          try { $ownerBefore = Get-Content -LiteralPath $ownerFile -Force -ErrorAction Stop | Select-Object -First 1 } catch { $ownerBefore = $null }
        }
        try {
          Move-Item -LiteralPath $ClaudeLock -Destination $staleLock -Force -ErrorAction Stop
          $ownerAfter = $null
          $movedOwnerFile = Join-Path $staleLock "owner"
          if (Test-Path -LiteralPath $movedOwnerFile) {
            try { $ownerAfter = Get-Content -LiteralPath $movedOwnerFile -Force -ErrorAction Stop | Select-Object -First 1 } catch { $ownerAfter = $null }
          }
          if ($ownerAfter -ne $ownerBefore) {
            if (-not (Test-Path -LiteralPath $ClaudeLock)) {
              Move-Item -LiteralPath $staleLock -Destination $ClaudeLock -Force -ErrorAction SilentlyContinue
            }
          } else {
            Remove-Item -LiteralPath $staleLock -Recurse -Force -ErrorAction SilentlyContinue
          }
        } catch {
          Write-Verbose "[sync-harness] could not evict the stale claude lock: $($_.Exception.Message)"
        }
        Start-Sleep -Milliseconds 200
        continue
      }
    }
    Start-Sleep -Milliseconds 200
  }
}
if (-not $locked) { Fail "[sync-harness] could not acquire the claude mirror lock at $ClaudeLock" }

try {
  Copy-NoReparse $SkillsSrc $ClaudeTmp
  # Compare the full relative file set (path + hash), not just top-level counts.
  $srcManifest = Get-TreeManifest $SkillsSrc
  $dstManifest = Get-TreeManifest $ClaudeTmp
  if (Compare-Object -ReferenceObject $srcManifest -DifferenceObject $dstManifest) {
    Fail "[sync-harness] claude mirror mismatch: source and copy differ"
  }
  # Move the live mirror aside before the swap (two renames, not atomic).
  $ClaudeOld = "$ClaudeSkills.old.$PID"
  if (Test-Path -LiteralPath $ClaudeSkills) { Move-Item -LiteralPath $ClaudeSkills -Destination $ClaudeOld -Force }
  try {
    Move-Item -LiteralPath $ClaudeTmp -Destination $ClaudeSkills -Force
  } catch {
    if (Test-Path -LiteralPath $ClaudeOld) { Move-Item -LiteralPath $ClaudeOld -Destination $ClaudeSkills -Force }
    throw
  }
} finally {
  if (Test-Path -LiteralPath $ClaudeTmp) { Remove-Item -LiteralPath $ClaudeTmp -Recurse -Force -ErrorAction SilentlyContinue }
  # Single owner of backup deletion: keep it only if the live mirror is missing
  # (a failed restore), since it is then the only copy and must survive.
  if ($ClaudeOld -and (Test-Path -LiteralPath $ClaudeOld) -and (Test-Path -LiteralPath $ClaudeSkills)) {
    Remove-Item -LiteralPath $ClaudeOld -Recurse -Force -ErrorAction SilentlyContinue
  }
  if (Test-Path -LiteralPath $ClaudeLock) {
    $ownerFile = Join-Path $ClaudeLock "owner"
    $owner = Get-Content -LiteralPath $ownerFile -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($owner -eq $lockToken) { Remove-Item -LiteralPath $ClaudeLock -Recurse -Force -ErrorAction SilentlyContinue }
  }
}

# Mirror the plugin commands into the harness project command directories.
$CmdsSrc = Join-Path $Root "plugins/hackathonkit/commands"
if (-not (Test-Path -LiteralPath $CmdsSrc)) {
  Fail "[sync-harness] commands source not found: $CmdsSrc"
}
foreach ($dest in @((Join-Path $Root ".opencode/commands"), (Join-Path $Root ".claude/commands"))) {
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Get-ChildItem -LiteralPath $CmdsSrc -Filter *.md | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $dest $_.Name) -Force
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
exit 0
