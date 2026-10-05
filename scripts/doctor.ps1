$ErrorActionPreference = "Continue"

# A bounded TCP connect is far faster than Test-NetConnection's ICMP+TCP probe.
function Test-TcpPort([string]$HostName, [int]$Port) {
  $client = $null
  $async = $null
  try {
    $client = New-Object System.Net.Sockets.TcpClient
    $async = $client.BeginConnect($HostName, $Port, $null, $null)
    if (-not $async.AsyncWaitHandle.WaitOne(1000)) {
      # Complete the pending connect to observe its outcome, then return; the
      # finally block releases the wait handle and closes the client exactly
      # once (Close() does not reliably abort an in-flight connect).
      try { $client.EndConnect($async) } catch { }
      return $false
    }
    $client.EndConnect($async)
    return $true
  } catch {
    return $false
  } finally {
    if ($async) { $async.AsyncWaitHandle.Close() }
    if ($client) { $client.Close() }
  }
}
$scriptPath = $PSCommandPath
if (-not $scriptPath) { $scriptPath = $MyInvocation.MyCommand.Path }
if (-not $scriptPath -and $PSScriptRoot) { $scriptPath = Join-Path $PSScriptRoot "doctor.ps1" }
if (-not $scriptPath) {
  Write-Host "[doctor] cannot resolve the script path; run this file, not dot-sourced" -ForegroundColor Red
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $scriptPath)
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
# Go is optional (the docker path does not need it), so report it as info
# rather than a check that can never fail.
Write-Host ("[info] {0,-22} {1}" -f "go toolchain (dev)", $(if ($go) { (go version) } else { "not found (not required for docker path)" }))

$codex = Get-Command codex -ErrorAction SilentlyContinue
Check "codex CLI" ($null -ne $codex) $(if ($codex) { (codex --version) } else { "not found" })

$docker = Get-Command docker -ErrorAction SilentlyContinue
$engine = $false
if ($docker) {
  docker info *> $null
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

# Get-FileHash raises for locked/unreadable files; under
# $ErrorActionPreference = "Continue" that would silently yield a null hash and
# let two unreadable files compare equal. Record every failure and force the
# mirror check to fail instead.
$script:hashErrors = 0
$srcSkillsPath = Join-Path $root "plugins\hackathonkit\skills"
$srcFiles = @()
if (Test-Path -LiteralPath $srcSkillsPath) {
  $srcFiles = @(Get-ChildItem -LiteralPath $srcSkillsPath -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($srcSkillsPath.Length).TrimStart('\', '/')
      try {
        "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256 -ErrorAction Stop).Hash)"
      } catch {
        $script:hashErrors++
        Write-Warning "[doctor] cannot hash $($_.FullName): $($_.Exception.Message)"
        "$rel|unreadable"
      }
    } | Sort-Object)
}
$dstPath = Join-Path $root ".agents\skills"
$dstFiles = @()
if (Test-Path -LiteralPath $dstPath) {
  $dstFiles = @(Get-ChildItem -LiteralPath $dstPath -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($dstPath.Length).TrimStart('\', '/')
      try {
        "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256 -ErrorAction Stop).Hash)"
      } catch {
        $script:hashErrors++
        Write-Warning "[doctor] cannot hash $($_.FullName): $($_.Exception.Message)"
        "$rel|unreadable"
      }
    } | Sort-Object)
}
$mirrorDiff = @(Compare-Object -ReferenceObject $srcFiles -DifferenceObject $dstFiles)
$mirrorOk = ($srcFiles.Count -gt 0) -and ($mirrorDiff.Count -eq 0) -and ($script:hashErrors -eq 0)
Check "skills mirror" $mirrorOk "plugin=$($srcFiles.Count) files fallback=$($dstFiles.Count)"

$hooksFile = Join-Path $root "plugins\hackathonkit\hooks\codex-hooks.json"
Check "session hooks" (Test-Path -LiteralPath $hooksFile) $(if (Test-Path -LiteralPath $hooksFile) { "codex-hooks.json" } else { "missing plugins/hackathonkit/hooks/codex-hooks.json" })

$verifyCmd = Join-Path $root "plugins\hackathonkit\commands\verify.md"
Check "verify command" (Test-Path -LiteralPath $verifyCmd) $(if (Test-Path -LiteralPath $verifyCmd) { "/verify runs 7 checks incl. coverage" } else { "missing plugins/hackathonkit/commands/verify.md; run scripts/repair.ps1" })

$verifyPrompt = Join-Path (Join-Path $HOME ".codex") "prompts\verify.md"
Check "command prompts (codex)" (Test-Path -LiteralPath $verifyPrompt) $(if (Test-Path -LiteralPath $verifyPrompt) { "installed in $HOME\.codex\prompts" } else { "not installed; run scripts/install-prompts.ps1 install" })

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
  try {
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($state.version) { $stateVersion = [string]$state.version }
  } catch {
    $stateVersion = "unreadable"
  }
}
Check "install state" ($stateVersion -ne "" -and $stateVersion -ne "unreadable") $(if ($stateVersion) { "version $stateVersion" } else { "missing configs/install-state.json; run scripts/setup.ps1" })

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
  $json = (& codex plugin list --json 2>$null) -join "`n"
  $codexExit = $LASTEXITCODE
  $pluginInstalled = $false
  $pluginEnabled = $false
  $pluginDetail = "install: scripts/setup.ps1 or codex plugin add hackathonkit@hackathonkit-local"
  if ($codexExit -ne 0) {
    $pluginDetail = "codex plugin list failed (exit $codexExit)"
  } elseif ($json) {
    try {
      $parsed = $json | ConvertFrom-Json
      if ($null -eq $parsed.installed) {
        $pluginDetail = "unexpected codex plugin list shape (no installed array)"
      } else {
        foreach ($p in $parsed.installed) {
          if ($p.name -eq "hackathonkit") {
            $pluginInstalled = $true
            $pluginEnabled = [bool]$p.enabled
          }
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
    $pluginCheckPath = Join-Path $root "scripts\plugin-check.ps1"
    if (-not (Test-Path -LiteralPath $pluginCheckPath)) {
      Check "plugin runtime" $false "scripts/plugin-check.ps1 is missing"
    } else {
      # Do not pre-set $LASTEXITCODE: a missing/failed check would then look
      # successful. Capture $? immediately after the call as well.
      $checkOutput = & $pluginCheckPath *>&1
      $ok = $?
      $pluginExit = $LASTEXITCODE
      if ($ok -and $pluginExit -eq 0) {
        Check "plugin runtime" $true "installed plugin starts MCP from a foreign cwd"
      } else {
        $reason = ($checkOutput | Where-Object { $_ } | Select-Object -Last 1)
        if (-not $reason) { $reason = "plugin-check failed (exit $pluginExit)" }
        Check "plugin runtime" $false "$reason"
      }
    }
  } else {
    Write-Host ("[info] {0,-22} skipped (plugin not installed or disabled)" -f "plugin runtime")
  }
}

if ($engine) {
  $pg = Test-TcpPort "127.0.0.1" 5432
  Write-Host ("[info] {0,-22} {1}" -f "postgres", $(if ($pg) { "localhost:5432 (optional metadata backend)" } else { "not running (optional metadata backend)" }))
  $minio = Test-TcpPort "127.0.0.1" 9000
  Write-Host ("[info] {0,-22} {1}" -f "minio", $(if ($minio) { "localhost:9000 (optional artifact backend)" } else { "not running (optional artifact backend)" }))
}

$smokePath = Join-Path $root "scripts\smoke.ps1"
if (-not (Test-Path -LiteralPath $smokePath)) {
  Check "mcp handshake" $false "scripts/smoke.ps1 is missing"
} else {
  # No pre-set $LASTEXITCODE: capture the real result (and $? as a backstop for
  # a script that early-returns without calling exit).
  & $smokePath -Quiet
  $smokeOk = $?
  $smokeExit = $LASTEXITCODE
  if (-not $smokeOk) { $smokeExit = 1 }
  Check "mcp handshake" ($smokeExit -eq 0) "initialize / tools/list / system.context"
}

Write-Host ""
if ($script:fail -eq 0) {
  Write-Host "[doctor] all checks passed"
  exit 0
}
Write-Host "[doctor] $($script:fail) check(s) failed"
exit 1
