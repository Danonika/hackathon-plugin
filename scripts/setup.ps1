param(
  [string]$Profile = "full"
)
$ErrorActionPreference = "Continue"
if ($Profile -notin @("core", "full")) {
  Write-Error "[setup] invalid profile: $Profile (expected core or full)"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
Set-Location -LiteralPath $root
Write-Host "[setup] repository root: $root (profile: $Profile)"

if (-not (Test-Path -LiteralPath ".env")) {
  Copy-Item -LiteralPath ".env.example" -Destination ".env"
  Write-Host "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
}

& (Join-Path $root "scripts\sync-harness.ps1")
if ($LASTEXITCODE -ne 0) { Write-Error "[setup] sync-harness failed"; exit 1 }

if ($Profile -eq "core") {
  foreach ($dir in @((Join-Path $root ".agents\skills"), (Join-Path $root ".claude\skills"))) {
    if (-not (Test-Path $dir)) { continue }
    foreach ($skill in @("judge", "project-cost", "roast-project", "cp-expert-refactoring", "system-design-expert", "self-improvement-controller", "output-self-check", "skill-authoring", "qa-expert")) {
      Remove-Item -Recurse -Force (Join-Path $dir $skill) -ErrorAction SilentlyContinue
    }
  }
  Write-Host "[setup] core profile: explicit-only evaluation skills excluded from mirrors (the plugin still ships all skills)"
}

& (Join-Path $root "scripts\gen-mcp-manifest.ps1") -Root $root
if ($LASTEXITCODE -ne 0) { Write-Error "[setup] manifest generation failed"; exit 1 }

New-Item -ItemType Directory -Force -Path (Join-Path $root "logs"), (Join-Path $root "evals"), (Join-Path $root "data\artifacts"), (Join-Path $root "data\index"), (Join-Path $root "data\control"), (Join-Path $root "data\judge-bundles") | Out-Null

$dockerOk = $false
if (Get-Command docker -ErrorAction SilentlyContinue) {
  docker info *> $null
  $dockerOk = ($LASTEXITCODE -eq 0)
}

$runtime = "binary"
if ($dockerOk) {
  $runtime = "docker"
  Write-Host "[setup] building MCP image"
  docker compose -f (Join-Path $root "docker-compose.yml") build hackathonkit-mcp
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] docker compose build failed"; exit 1 }
  Write-Host "[setup] starting infrastructure (postgres, redis, minio)"
  docker compose -f (Join-Path $root "docker-compose.yml") up -d postgres redis minio
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] docker compose up failed"; exit 1 }
} else {
  Write-Warning "[setup] docker engine is not available; building local binaries instead"
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "[setup] go toolchain not found; install Go or start Docker, then re-run setup"
    exit 1
  }
  New-Item -ItemType Directory -Force -Path (Join-Path $root "bin") | Out-Null
  go build -o bin/hackathonkit-mcp.exe ./cmd/hackathonkit-mcp
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] go build hackathonkit-mcp failed"; exit 1 }
  go build -o bin/hackathonkit-demo-api.exe ./cmd/demo-api
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] go build demo-api failed"; exit 1 }
  go build -o bin/hackathonkit-ctl.exe ./cmd/hackathonkit-ctl
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] go build hackathonkit-ctl failed"; exit 1 }
  Write-Host "[setup] local binaries built in bin/"
}

& (Join-Path $root "scripts\write-install-state.ps1") -Root $root -Profile $Profile -Runtime $runtime

$codex = Get-Command codex -ErrorAction SilentlyContinue
if ($codex) {
  Write-Host "[setup] registering repo marketplace and installing the Hackathon Agent Kit plugin"
  & codex plugin remove hackathonkit *> $null
  & codex plugin marketplace add $root *> $null
  if ($LASTEXITCODE -ne 0) {
    Write-Warning "[setup] codex plugin marketplace add failed (exit $LASTEXITCODE)"
  }
  & codex plugin add hackathonkit@hackathonkit-local *> $null
  if ($LASTEXITCODE -eq 0) {
    Write-Host "[setup] plugin installed; start a NEW Codex session to load it"
  } else {
    Write-Warning "[setup] could not install the plugin automatically. Run manually: codex plugin marketplace add '$root'; codex plugin add hackathonkit@hackathonkit-local"
  }
} else {
  Write-Warning "[setup] codex CLI not found; install it (npm install -g @openai/codex) and re-run setup"
}

Write-Host "[setup] done."
Write-Host "Next: open Codex from the repository root and start a NEW Codex session."
