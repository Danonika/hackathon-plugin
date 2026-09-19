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
  $pluginInstalled = $false
  $pluginEnabled = $false
  if ($json) {
    try {
      $parsed = $json | ConvertFrom-Json
      foreach ($p in $parsed.installed) {
        if ($p.name -eq "hackathonkit") {
          $pluginInstalled = [bool]$p.installed
          $pluginEnabled = [bool]$p.enabled
        }
      }
    } catch { }
  }
  Check "hackathonkit plugin" ($pluginInstalled -and $pluginEnabled) "install: scripts/setup.ps1 or codex plugin add hackathonkit@hackathonkit-local"

  & (Join-Path $root "scripts\plugin-check.ps1") | Out-Null
  Check "plugin runtime" ($LASTEXITCODE -eq 0) "installed plugin starts MCP from a foreign cwd (scripts/plugin-check.ps1)"
}

if ($engine) {
  $pg = Test-NetConnection 127.0.0.1 -Port 5432 -InformationLevel Quiet -WarningAction SilentlyContinue
  Check "postgres" $pg "localhost:5432 (optional metadata backend)"
  $minio = Test-NetConnection 127.0.0.1 -Port 9000 -InformationLevel Quiet -WarningAction SilentlyContinue
  Check "minio" $minio "localhost:9000 (optional artifact backend)"
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
