$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
Write-Host "[repair] repository root: $root"

& (Join-Path $root "scripts\sync-harness.ps1")
& (Join-Path $root "scripts\gen-mcp-manifest.ps1") -Root $root
New-Item -ItemType Directory -Force -Path (Join-Path $root "logs"), (Join-Path $root "evals"), (Join-Path $root "data\artifacts"), (Join-Path $root "data\index"), (Join-Path $root "data\control"), (Join-Path $root "data\judge-bundles") | Out-Null

$profile = "full"
$statePath = Join-Path $root "configs\install-state.json"
if (Test-Path $statePath) {
  $match = Select-String -Path $statePath -Pattern '"profile"\s*:\s*"([^"]+)"' | Select-Object -First 1
  if ($match) { $profile = $match.Matches[0].Groups[1].Value }
}

$runtime = "binary"
if (Get-Command docker -ErrorAction SilentlyContinue) {
  cmd /c "docker info >nul 2>nul"
  if ($LASTEXITCODE -eq 0) { $runtime = "docker" }
}

$bin = Join-Path $root "bin\hackathonkit-mcp.exe"
if (-not (Test-Path $bin) -and $runtime -eq "binary") {
  if (Get-Command go -ErrorAction SilentlyContinue) {
    New-Item -ItemType Directory -Force -Path (Join-Path $root "bin") | Out-Null
    go build -o bin/hackathonkit-mcp.exe ./cmd/hackathonkit-mcp
    go build -o bin/hackathonkit-demo-api.exe ./cmd/demo-api
    go build -o bin/hackathonkit-ctl.exe ./cmd/hackathonkit-ctl
    Write-Host "[repair] rebuilt local binaries in bin/"
  } else {
    Write-Warning "[repair] no Go toolchain and no docker engine; MCP runtime unavailable"
  }
}

& (Join-Path $root "scripts\write-install-state.ps1") -Root $root -Profile $profile -Runtime $runtime

if (Get-Command codex -ErrorAction SilentlyContinue) {
  cmd /c "codex plugin marketplace add `"$root`" >nul 2>nul"
  cmd /c "codex plugin add hackathonkit@hackathonkit-local >nul 2>nul"
  if ($LASTEXITCODE -eq 0) { Write-Host "[repair] plugin reinstalled; start a NEW Codex session" }
}

Write-Host "[repair] done. Run scripts/doctor.ps1 to verify."
