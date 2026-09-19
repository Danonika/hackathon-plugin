$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
$script:fail = 0

function Check([string]$name, [bool]$ok, [string]$detail) {
  if ($ok) {
    Write-Host ("[ok]   {0,-22} {1}" -f $name, $detail)
  } else {
    Write-Host ("[fail] {0,-22} {1}" -f $name, $detail)
    $script:fail++
  }
}

Check "repository root" (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml")) $root

$routingFile = Join-Path $root "configs\routing.yaml"
Check "routing config" (Test-Path -LiteralPath $routingFile) $routingFile

$go = Get-Command go -ErrorAction SilentlyContinue
Check "go toolchain (dev)" ($null -ne $go) $(if ($go) { (go version) } else { "not found (not required for docker path)" })

$codex = Get-Command codex -ErrorAction SilentlyContinue
Check "codex CLI" ($null -ne $codex) $(if ($codex) { (codex --version) } else { "not found" })

$docker = Get-Command docker -ErrorAction SilentlyContinue
$engine = $false
if ($docker) {
  cmd /c "docker info >nul 2>nul"
  $engine = ($LASTEXITCODE -eq 0)
}
Check "docker engine" $engine $(if ($engine) { "running" } else { "not running" })

$binary = Test-Path -LiteralPath (Join-Path $root "bin\hackathonkit-mcp.exe")
$image = $false
if ($engine) {
  $img = cmd /c "docker images -q hackathon-agent-kit-hackathonkit-mcp 2>nul"
  $image = -not [string]::IsNullOrWhiteSpace($img)
}
Check "mcp runtime" ($binary -or $image) $(if ($binary) { "local binary" } elseif ($image) { "docker image" } else { "run scripts/setup.ps1" })

$srcSkills = (Get-ChildItem -LiteralPath (Join-Path $root "plugins\hackathonkit\skills") -Directory -ErrorAction SilentlyContinue).Count
$dstPath = Join-Path $root ".agents\skills"
$dstSkills = 0
if (Test-Path -LiteralPath $dstPath) {
  $dstSkills = (Get-ChildItem -LiteralPath $dstPath -Directory).Count
}
Check "skills mirror" ($srcSkills -gt 0 -and $srcSkills -eq $dstSkills) "plugin=$srcSkills fallback=$dstSkills"

if ($codex) {
  $json = (cmd /c "codex plugin list --json 2>nul") -join "`n"
  if ($json -match '"name"\s*:\s*"hackathonkit"') {
    if ($json -match '"enabled"\s*:\s*true') {
      Write-Host "[info] plugin hackathonkit            installed and enabled"
    } else {
      Write-Host "[info] plugin hackathonkit            installed, enable it in Codex config"
    }
  } else {
    Write-Host "[info] plugin hackathonkit            not installed: open Codex, install from repo marketplace, start a NEW session"
  }
}

& (Join-Path $root "scripts\smoke.ps1") -Quiet
Check "mcp handshake" ($LASTEXITCODE -eq 0) "initialize / tools/list / system.context"

Write-Host ""
if ($script:fail -eq 0) {
  Write-Host "[doctor] all checks passed"
  exit 0
}
Write-Host "[doctor] $($script:fail) check(s) failed"
exit 1
