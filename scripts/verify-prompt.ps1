# Install/remove the Hackathon Agent Kit /verify custom prompt for Codex.
#
# Codex plugins bundle skills, MCP servers and hooks, but not slash commands;
# custom prompts live in $CODEX_HOME/prompts (default ~/.codex/prompts).
#
# Usage: verify-prompt.ps1 [install|remove|status]
param(
  [string]$Action = "install"
)
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Src = Join-Path $Root ".codex\prompts\verify.md"
$CodexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME ".codex" }
$DestDir = Join-Path $CodexHome "prompts"
$Dest = Join-Path $DestDir "verify.md"

switch ($Action) {
  "install" {
    if (-not (Test-Path -LiteralPath $Src)) { Fail "[verify-prompt] source not found: $Src" }
    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
    Copy-Item -LiteralPath $Src -Destination $Dest -Force
    Write-Host "[verify-prompt] installed $Dest"
  }
  "remove" {
    if ((Test-Path -LiteralPath $Dest) -and (Test-Path -LiteralPath $Src)) {
      $same = (Get-FileHash -LiteralPath $Src -Algorithm SHA256).Hash -eq (Get-FileHash -LiteralPath $Dest -Algorithm SHA256).Hash
      if ($same) {
        Remove-Item -LiteralPath $Dest -Force
        Write-Host "[verify-prompt] removed $Dest"
      } else {
        Write-Host "[verify-prompt] left $Dest (user-modified)"
      }
    } else {
      Write-Host "[verify-prompt] left $Dest (absent or user-modified)"
    }
  }
  "status" {
    if (Test-Path -LiteralPath $Dest) {
      Write-Host "installed: $Dest"
    } else {
      Write-Host "not installed: $Dest (run scripts/verify-prompt.ps1 install)"
      exit 1
    }
  }
  default {
    Fail "usage: verify-prompt.ps1 [install|remove|status]"
  }
}
exit 0
