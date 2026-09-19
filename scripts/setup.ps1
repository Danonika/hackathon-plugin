$ErrorActionPreference = "Continue"
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
Set-Location -LiteralPath $root
Write-Host "[setup] repository root: $root"

if (-not (Test-Path -LiteralPath ".env")) {
  Copy-Item -LiteralPath ".env.example" -Destination ".env"
  Write-Host "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
}

& (Join-Path $root "scripts\sync-skills.ps1")
if ($LASTEXITCODE -ne 0) { Write-Error "[setup] sync-skills failed"; exit 1 }

$dockerOk = $false
if (Get-Command docker -ErrorAction SilentlyContinue) {
  cmd /c "docker info >nul 2>nul"
  $dockerOk = ($LASTEXITCODE -eq 0)
}

if ($dockerOk) {
  Write-Host "[setup] building MCP image"
  docker compose -f (Join-Path $root "docker-compose.yml") build hackathonkit-mcp
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] docker compose build failed"; exit 1 }
  Write-Host "[setup] starting infrastructure (postgres, redis, minio)"
  docker compose -f (Join-Path $root "docker-compose.yml") up -d postgres redis minio
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] docker compose up failed"; exit 1 }
} else {
  Write-Warning "[setup] docker engine is not available; building local binaries instead"
  New-Item -ItemType Directory -Force -Path (Join-Path $root "bin") | Out-Null
  go build -o bin/hackathonkit-mcp.exe ./cmd/hackathonkit-mcp
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] go build hackathonkit-mcp failed"; exit 1 }
  go build -o bin/hackathonkit-demo-api.exe ./cmd/demo-api
  if ($LASTEXITCODE -ne 0) { Write-Error "[setup] go build demo-api failed"; exit 1 }
  Write-Host "[setup] local binaries built in bin/"
}

Write-Host "[setup] done."
Write-Host "Next: open Codex from the repository root, install the Hackathon Agent Kit plugin from the repo marketplace (or use the .codex/config.toml fallback), start a NEW Codex session."
