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
& (Join-Path $root "scripts\gen-mcp-manifest.ps1") -Root $root
if ($LASTEXITCODE -ne 0) { Write-Error "[repair] manifest generation failed"; exit 1 }
New-Item -ItemType Directory -Force -Path (Join-Path $root "logs"), (Join-Path $root "evals"), (Join-Path $root "data\artifacts"), (Join-Path $root "data\index"), (Join-Path $root "data\control"), (Join-Path $root "data\judge-bundles") | Out-Null

$profile = "full"
$statePath = Join-Path $root "configs\install-state.json"
if (Test-Path $statePath) {
  $match = Select-String -Path $statePath -Pattern '"profile"\s*:\s*"([^"]+)"' | Select-Object -First 1
  if ($match) { $profile = $match.Matches[0].Groups[1].Value }
}

$runtime = "binary"
if (Get-Command docker -ErrorAction SilentlyContinue) {
  docker info *> $null
  if ($LASTEXITCODE -eq 0) { $runtime = "docker" }
}

$bin = Join-Path $root "bin\hackathonkit-mcp.exe"
if (-not (Test-Path $bin) -and $runtime -eq "binary") {
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
    Write-Warning "[repair] no Go toolchain and no docker engine; MCP runtime unavailable"
  }
}

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\write-install-state.ps1") -Root $root -Profile $profile -Runtime $runtime
if ($LASTEXITCODE -ne 0) { Write-Error "[repair] write-install-state failed"; exit 1 }

if (Get-Command codex -ErrorAction SilentlyContinue) {
  & codex plugin marketplace add $root *> $null
  if ($LASTEXITCODE -ne 0) { Write-Warning "[repair] codex plugin marketplace add failed (exit $LASTEXITCODE)" }
  & codex plugin add hackathonkit@hackathonkit-local *> $null
  if ($LASTEXITCODE -eq 0) {
    Write-Host "[repair] plugin reinstalled; start a NEW Codex session"
  } else {
    Write-Warning "[repair] could not reinstall the plugin automatically"
  }
}

Write-Host "[repair] done. Run scripts/doctor.ps1 to verify."
