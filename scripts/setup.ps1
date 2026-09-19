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

$template = Get-Content -LiteralPath (Join-Path $root "plugins\hackathonkit\.mcp.json.template") -Raw
$template.Replace("{{REPO_ROOT}}", $root.Replace("\", "/")) |
  Set-Content -LiteralPath (Join-Path $root "plugins\hackathonkit\.mcp.json") -NoNewline -Encoding UTF8
Write-Host "[setup] generated plugins/hackathonkit/.mcp.json with the absolute repository root"

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

$codex = Get-Command codex -ErrorAction SilentlyContinue
if ($codex) {
  Write-Host "[setup] registering repo marketplace and installing the Hackathon Agent Kit plugin"
  cmd /c "codex plugin remove hackathonkit >nul 2>nul"
  cmd /c "codex plugin marketplace add `"$root`" >nul 2>nul"
  cmd /c "codex plugin add hackathonkit@hackathonkit-local >nul 2>nul"
  if ($LASTEXITCODE -eq 0) {
    Write-Host "[setup] plugin installed; start a NEW Codex session to load it"
  } else {
    Write-Warning "[setup] could not install the plugin automatically. Run manually: codex plugin marketplace add `"$root`"; codex plugin add hackathonkit@hackathonkit-local"
  }
} else {
  Write-Warning "[setup] codex CLI not found; install it (npm install -g @openai/codex) and re-run setup"
}

Write-Host "[setup] done."
Write-Host "Next: open Codex from the repository root and start a NEW Codex session."
