$ErrorActionPreference = "Continue"
if (-not $PSCommandPath) {
  Write-Error "[repair] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Write-Error "[repair] refusing: '$root' does not look like the hackathonkit repository root"
  exit 1
}
Set-Location -LiteralPath $root
Write-Host "[repair] repository root: $root"

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\sync-harness.ps1")
if ($LASTEXITCODE -ne 0) { Write-Error "[repair] sync-harness failed"; exit 1 }
$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\gen-mcp-manifest.ps1") -Root $root
if ($LASTEXITCODE -ne 0) { Write-Error "[repair] manifest generation failed"; exit 1 }
& (Join-Path $root "scripts\verify-prompt.ps1") install
if ($LASTEXITCODE -ne 0) { Write-Warning "[repair] could not install the Codex /verify prompt" }
$dirFailures = 0
foreach ($d in @("logs", "evals", "data\artifacts", "data\index", "data\control", "data\judge-bundles")) {
  try {
    New-Item -ItemType Directory -Force -Path (Join-Path $root $d) -ErrorAction Stop | Out-Null
  } catch {
    Write-Warning "[repair] cannot create ${d}: $($_.Exception.Message)"
    $dirFailures++
  }
}
if ($dirFailures -gt 0) { Write-Error "[repair] $dirFailures directory creation failure(s)"; exit 1 }

$profile = "full"
$statePath = Join-Path $root "configs\install-state.json"
if (Test-Path -LiteralPath $statePath) {
  try {
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($state.profile -in @("core", "full")) {
      $profile = $state.profile
    } elseif ($state.profile) {
      Write-Warning "[repair] unknown profile '$($state.profile)' in install-state; using 'full'"
    }
  } catch {
    Write-Warning "[repair] could not read the install state; using 'full'"
  }
}

$runtime = "binary"
if (Get-Command docker -ErrorAction SilentlyContinue) {
  docker info *> $null
  if ($LASTEXITCODE -eq 0) { $runtime = "docker" }
}

$bin = Join-Path $root "bin\hackathonkit-mcp.exe"
$demo = Join-Path $root "bin\hackathonkit-demo-api.exe"
$ctl = Join-Path $root "bin\hackathonkit-ctl.exe"
if ((-not (Test-Path $bin)) -or (-not (Test-Path $demo)) -or (-not (Test-Path $ctl))) {
  if ($runtime -eq "binary") {
    if (Get-Command go -ErrorAction SilentlyContinue) {
      New-Item -ItemType Directory -Force -Path (Join-Path $root "bin") | Out-Null
      go build -o bin/hackathonkit-mcp.exe ./cmd/hackathonkit-mcp
      if ($LASTEXITCODE -ne 0) { Write-Error "[repair] go build hackathonkit-mcp failed"; exit 1 }
      go build -o bin/hackathonkit-demo-api.exe ./cmd/demo-api
      if ($LASTEXITCODE -ne 0) { Write-Error "[repair] go build demo-api failed"; exit 1 }
      go build -o bin/hackathonkit-ctl.exe ./cmd/hackathonkit-ctl
      if ($LASTEXITCODE -ne 0) { Write-Error "[repair] go build hackathonkit-ctl failed"; exit 1 }
      Write-Host "[repair] rebuilt local binaries in bin/"
    } else {
      Write-Error "[repair] no Go toolchain and no docker engine; MCP runtime unavailable"
      exit 1
    }
  } else {
    Write-Host "[repair] docker runtime is active; local binaries are not required"
  }
}

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\write-install-state.ps1") -Root $root -Profile $profile -Runtime $runtime
if ($LASTEXITCODE -ne 0) { Write-Error "[repair] write-install-state failed"; exit 1 }

if (Get-Command codex -CommandType Application -ErrorAction SilentlyContinue) {
  $global:LASTEXITCODE = 0
  & codex plugin marketplace add $root *> $null
  $marketExit = $LASTEXITCODE
  if ($marketExit -ne 0) { Write-Warning "[repair] codex plugin marketplace add failed (exit $marketExit)" }
  $global:LASTEXITCODE = 0
  & codex plugin add hackathonkit@hackathonkit-local *> $null
  $addExit = $LASTEXITCODE
  if ($addExit -eq 0) {
    Write-Host "[repair] plugin reinstalled; start a NEW Codex session"
  } else {
    Write-Warning "[repair] could not reinstall the plugin automatically (marketplace exit $marketExit, add exit $addExit)"
  }
}

Write-Host "[repair] done. Run scripts/doctor.ps1 to verify."
