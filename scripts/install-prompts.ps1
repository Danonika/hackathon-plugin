# Install/remove the Hackathon Agent Kit command prompts for Codex.
#
# Codex plugins bundle skills, MCP servers and hooks, but not slash commands;
# custom prompts live in $CODEX_HOME/prompts (default ~/.codex/prompts). The
# source of truth is plugins/hackathonkit/commands/*.md.
#
# Usage: install-prompts.ps1 [install|remove|status]
param(
  [string]$Action = "install"
)
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  [Console]::Error.WriteLine($Message)
  exit 1
}

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$SrcDir = Join-Path $Root "plugins/hackathonkit/commands"
if ($env:CODEX_HOME) {
  $CodexHome = $env:CODEX_HOME
} elseif ($HOME) {
  $CodexHome = Join-Path $HOME ".codex"
} else {
  Fail "[prompts] HOME is not set and CODEX_HOME is empty; cannot resolve the prompts directory"
}
if (-not [System.IO.Path]::IsPathRooted($CodexHome)) {
  Fail "[prompts] the prompts home must be an absolute path: $CodexHome"
}
# IsPathRooted accepts drive-relative specs like "C:", which Join-Path would
# resolve against the current directory on that drive; require a full path.
if ($CodexHome -match '^[A-Za-z]:$') {
  Fail "[prompts] the prompts home must be a fully rooted path, not a bare drive: $CodexHome"
}
$DestDir = Join-Path $CodexHome "prompts"

if (-not (Test-Path -LiteralPath $SrcDir)) { Fail "[prompts] source not found: $SrcDir" }
$Sources = @(Get-ChildItem -LiteralPath $SrcDir -Filter *.md -File)
if ($Sources.Count -eq 0) { Fail "[prompts] no *.md prompts found in $SrcDir" }

switch ($Action) {
  "install" {
    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
    $skipped = 0
    foreach ($f in $Sources) {
      $dest = Join-Path $DestDir $f.Name
      $existing = Get-Item -LiteralPath $dest -Force -ErrorAction SilentlyContinue
      if ($existing) {
        # A directory or symlink/junction destination would make Copy-Item write
        # into it or follow the link; refuse rather than clobber an unexpected
        # target. Parity with scripts/install-prompts.sh.
        if ($existing.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
          Write-Warning "[prompts] skipping ${dest} (reparse point)"
          $skipped++
          continue
        }
        if ($existing.PSIsContainer) {
          Write-Warning "[prompts] skipping ${dest} (not a regular file)"
          $skipped++
          continue
        }
        try {
          $same = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash -eq (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash
          if (-not $same) { Write-Warning "[prompts] warning: overwriting locally modified prompt $dest" }
        } catch {
          Write-Warning "[prompts] warning: could not compare ${dest}: $($_.Exception.Message)"
        }
      }
      Copy-Item -LiteralPath $f.FullName -Destination $dest -Force
    }
    Write-Host "[prompts] installed $($Sources.Count) prompt(s) to $DestDir"
    if ($skipped -gt 0) { Write-Warning "[prompts] skipped $skipped non-regular destination(s)" }
  }
  "remove" {
    $removed = 0
    $kept = 0
    foreach ($f in $Sources) {
      $dest = Join-Path $DestDir $f.Name
      if (-not (Test-Path -LiteralPath $dest)) { continue }
      try {
        $same = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash -eq (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash
      } catch {
        Write-Warning "[prompts] could not compare ${dest}: $($_.Exception.Message)"
        $kept++
        continue
      }
      if ($same) {
        try { Remove-Item -LiteralPath $dest -Force -ErrorAction Stop; $removed++ }
        catch { Write-Warning "[prompts] could not remove ${dest}: $($_.Exception.Message)"; $kept++ }
      } else {
        Write-Warning "[prompts] keeping modified prompt: $dest"
        $kept++
      }
    }
    $detail = if ($kept -gt 0) { " ($kept kept)" } else { "" }
    Write-Host "[prompts] removed $removed prompt(s) from $DestDir$detail"
  }
  "status" {
    $missing = @()
    $outdated = @()
    foreach ($f in $Sources) {
      $dest = Join-Path $DestDir $f.Name
      if (-not (Test-Path -LiteralPath $dest)) { $missing += $f.Name; continue }
      try {
        if ((Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash) {
          $outdated += $f.Name
        }
      } catch {
        $outdated += $f.Name
      }
    }
    if ($missing.Count -eq 0 -and $outdated.Count -eq 0) {
      Write-Host "installed: $DestDir"
    } else {
      $parts = @()
      if ($missing.Count -gt 0) { $parts += "$($missing.Count) missing" }
      if ($outdated.Count -gt 0) { $parts += "$($outdated.Count) out of date" }
      [Console]::Error.WriteLine("not installed ($($parts -join ', ')): $DestDir (run scripts/install-prompts.ps1 install)")
      exit 1
    }
  }
  default {
    Fail "usage: install-prompts.ps1 [install|remove|status]"
  }
}
exit 0
