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
Check "go toolchain (dev)" $true $(if ($go) { (go version) } else { "not found (not required for docker path)" })

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
  $img = docker images --filter "reference=*hackathonkit-mcp*" -q 2>$null
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

$hooksFile = Join-Path $root "plugins\hackathonkit\hooks\codex-hooks.json"
Check "session hooks" (Test-Path -LiteralPath $hooksFile) $(if (Test-Path -LiteralPath $hooksFile) { "codex-hooks.json" } else { "missing plugins/hackathonkit/hooks/codex-hooks.json" })

$agentCount = 0
$agentsDir = Join-Path $root ".codex\agents"
if (Test-Path -LiteralPath $agentsDir) {
  $agentCount = (Get-ChildItem -LiteralPath $agentsDir -Filter *.toml).Count
}
Check "codex subagents" ($agentCount -ge 3) "$agentCount roles"

$adapterFiles = @("AGENTS.md", ".claude-plugin\plugin.json", ".claude-plugin\marketplace.json", ".opencode\opencode.json", ".cursor\rules\hackathonkit.mdc")
$adapterMissing = 0
foreach ($f in $adapterFiles) {
  if (-not (Test-Path -LiteralPath (Join-Path $root $f))) { $adapterMissing++ }
}
Check "harness adapters" ($adapterMissing -eq 0) $(if ($adapterMissing -eq 0) { "codex, opencode, claude, cursor" } else { "$adapterMissing missing; run scripts/sync-harness.ps1" })

$statePath = Join-Path $root "configs\install-state.json"
$stateVersion = ""
if (Test-Path -LiteralPath $statePath) {
  $stateMatch = Select-String -Path $statePath -Pattern '"version"\s*:\s*"([^"]+)"' | Select-Object -First 1
  if ($stateMatch) { $stateVersion = $stateMatch.Matches[0].Groups[1].Value }
}
Check "install state" (Test-Path -LiteralPath $statePath) $(if ($stateVersion) { "version $stateVersion" } else { "missing configs/install-state.json; run scripts/setup.ps1" })

$telegramSet = $false
if ($env:TELEGRAM_BOT_TOKEN) { $telegramSet = $true }
elseif (Test-Path (Join-Path $root ".env")) {
  $telegramSet = [bool](Select-String -Path (Join-Path $root ".env") -Pattern "^TELEGRAM_BOT_TOKEN=.+" -Quiet)
}
Write-Host ("[info] {0,-22} {1}" -f "telegram", $(if ($telegramSet) { "configured" } else { "not configured (set TELEGRAM_BOT_TOKEN in .env)" }))

$node = Get-Command node -ErrorAction SilentlyContinue
Check "node (hooks)" ($null -ne $node) $(if ($node) { (node --version) } else { "not found; session hooks require Node 18+" })
if ($node) {
  & node (Join-Path $root "plugins\hackathonkit\scripts\hooks.test.js") *> $null
  Check "hook behavior" ($LASTEXITCODE -eq 0) "session ownership test"
}

$mcpExe = Join-Path $root "bin\hackathonkit-mcp.exe"
if (Test-Path -LiteralPath $mcpExe) {
  & $mcpExe --repo-root $root --validate-skills *> $null
  Check "skill standard" ($LASTEXITCODE -eq 0) "catalog passes docs/SKILL-STANDARD.md"
} else {
  Write-Host ("[info] {0,-22} skipped (no local binary; run scripts/setup.ps1)" -f "skill standard")
}

if ($codex) {
  $json = (cmd /c "codex plugin list --json 2>nul") -join "`n"
  $pluginInstalled = $false
  $pluginEnabled = $false
  $pluginDetail = "install: scripts/setup.ps1 or codex plugin add hackathonkit@hackathonkit-local"
  if ($json) {
    try {
      $parsed = $json | ConvertFrom-Json
      foreach ($p in $parsed.installed) {
        if ($p.name -eq "hackathonkit") {
          $pluginInstalled = $true
          $pluginEnabled = [bool]$p.enabled
        }
      }
    } catch {
      $pluginDetail = "cannot parse codex plugin list output: $($_.Exception.Message)"
    }
  } else {
    $pluginDetail = "codex plugin list returned no output"
  }
  if ($pluginInstalled -and -not $pluginEnabled) {
    $pluginDetail = "installed but disabled; enable it in Codex"
  } elseif ($pluginInstalled) {
    $pluginDetail = "installed and enabled"
  }
  Check "hackathonkit plugin" ($pluginInstalled -and $pluginEnabled) $pluginDetail

  if ($pluginInstalled -and $pluginEnabled) {
    $checkOutput = & (Join-Path $root "scripts\plugin-check.ps1") *>&1
    if ($LASTEXITCODE -eq 0) {
      Check "plugin runtime" $true "installed plugin starts MCP from a foreign cwd"
    } else {
      $reason = ($checkOutput | Where-Object { $_ } | Select-Object -Last 1)
      Check "plugin runtime" $false "$reason"
    }
  } else {
    Write-Host ("[info] {0,-22} skipped (plugin not installed or disabled)" -f "plugin runtime")
  }
}

if ($engine) {
  $pg = Test-NetConnection 127.0.0.1 -Port 5432 -InformationLevel Quiet -WarningAction SilentlyContinue
  Write-Host ("[info] {0,-22} {1}" -f "postgres", $(if ($pg) { "localhost:5432 (optional metadata backend)" } else { "not running (optional metadata backend)" }))
  $minio = Test-NetConnection 127.0.0.1 -Port 9000 -InformationLevel Quiet -WarningAction SilentlyContinue
  Write-Host ("[info] {0,-22} {1}" -f "minio", $(if ($minio) { "localhost:9000 (optional artifact backend)" } else { "not running (optional artifact backend)" }))
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
