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
  Write-Host $Message -ForegroundColor Red
  exit 1
}

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$SrcDir = Join-Path $Root "plugins\hackathonkit\commands"
$CodexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME ".codex" }
$DestDir = Join-Path $CodexHome "prompts"

if (-not (Test-Path -LiteralPath $SrcDir)) { Fail "[prompts] source not found: $SrcDir" }
$Sources = @(Get-ChildItem -LiteralPath $SrcDir -Filter *.md)

switch ($Action) {
  "install" {
    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
    foreach ($f in $Sources) {
      Copy-Item -LiteralPath $f.FullName -Destination (Join-Path $DestDir $f.Name) -Force
    }
    Write-Host "[prompts] installed $($Sources.Count) prompt(s) to $DestDir"
  }
  "remove" {
    foreach ($f in $Sources) {
      $dest = Join-Path $DestDir $f.Name
      if (Test-Path -LiteralPath $dest) {
        $same = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash -eq (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash
        if ($same) { Remove-Item -LiteralPath $dest -Force }
      }
    }
    Write-Host "[prompts] removed unmodified prompts from $DestDir"
  }
  "status" {
    $missing = @($Sources | Where-Object { -not (Test-Path -LiteralPath (Join-Path $DestDir $_.Name)) })
    if ($missing.Count -eq 0) {
      Write-Host "installed: $DestDir"
    } else {
      Write-Host "not installed ($($missing.Count) missing): $DestDir (run scripts/install-prompts.ps1 install)"
      exit 1
    }
  }
  default {
    Fail "usage: install-prompts.ps1 [install|remove|status]"
  }
}
exit 0
