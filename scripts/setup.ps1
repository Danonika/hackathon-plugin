param(
  [string]$Profile = "full"
)
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

if ($Profile -notin @("core", "full")) {
  Fail "[setup] invalid profile: $Profile (expected core or full)"
}
if (-not $PSCommandPath) {
  Fail "[setup] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Fail "[setup] refusing: '$root' does not look like the hackathonkit repository root"
}
$rootFull = [System.IO.Path]::GetFullPath($root)
Set-Location -LiteralPath $rootFull
$cwd = [System.IO.Path]::GetFullPath((Get-Location).Path)
if (-not $cwd.Equals($rootFull, [System.StringComparison]::OrdinalIgnoreCase)) {
  Fail "[setup] could not enter the repository root: $root"
}
Write-Host "[setup] repository root: $root (profile: $Profile)"

if (-not (Test-Path -LiteralPath ".env")) {
  if (-not (Test-Path -LiteralPath ".env.example")) {
    Fail "[setup] .env.example is missing; cannot create .env"
  }
  Copy-Item -LiteralPath ".env.example" -Destination ".env" -ErrorAction Stop
  if (-not (Test-Path -LiteralPath ".env")) {
    Fail "[setup] could not create .env from .env.example"
  }
  Write-Host "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
}

# A generated demo token protects the demo API without manual steps. The demo
# API fails closed on an empty token, so generate one when the shell bootstrap
# would (parity with scripts/setup.sh).
$envPath = Join-Path $root ".env"
$hasToken = $false
if (Test-Path -LiteralPath $envPath) {
  $hasToken = [bool](Select-String -LiteralPath $envPath -Pattern '^HACKATHONKIT_DEMO_TOKEN=.' -Quiet -ErrorAction SilentlyContinue)
}
if (-not $hasToken) {
  $bytes = New-Object byte[] 16
  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
  $token = -join ($bytes | ForEach-Object { $_.ToString("x2") })
  try {
    $lines = @()
    if (Test-Path -LiteralPath $envPath) { $lines = @(Get-Content -LiteralPath $envPath) }
    $replaced = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
      if ($lines[$i] -match '^HACKATHONKIT_DEMO_TOKEN=') { $lines[$i] = "HACKATHONKIT_DEMO_TOKEN=$token"; $replaced = $true }
    }
    if (-not $replaced) { $lines += "HACKATHONKIT_DEMO_TOKEN=$token" }
    Set-Content -LiteralPath $envPath -Value $lines
    Write-Host "[setup] generated HACKATHONKIT_DEMO_TOKEN for the demo API"
  } catch {
    Write-Warning "[setup] could not generate a demo token; set HACKATHONKIT_DEMO_TOKEN manually"
  }
}

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\sync-harness.ps1")
if ($LASTEXITCODE -ne 0) { Fail "[setup] sync-harness failed" }

# Codex plugins cannot bundle slash commands; install /verify as a custom prompt.
& (Join-Path $root "scripts\install-prompts.ps1") install
if ($LASTEXITCODE -ne 0) { Write-Warning "[setup] could not install the Codex command prompts" }

if ($Profile -eq "core") {
  foreach ($dir in @((Join-Path $root ".agents\skills"), (Join-Path $root ".claude\skills"))) {
    if (-not (Test-Path $dir)) { continue }
    $dirFull = [System.IO.Path]::GetFullPath($dir).TrimEnd([System.IO.Path]::DirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
    foreach ($skill in @("judge", "project-cost", "roast-project", "cp-expert-refactoring", "system-design-expert", "self-improvement-controller", "output-self-check", "skill-authoring", "qa-expert")) {
      $skillPath = Join-Path $dir $skill
      $skillFull = [System.IO.Path]::GetFullPath($skillPath)
      if (-not $skillFull.StartsWith($dirFull, [System.StringComparison]::OrdinalIgnoreCase)) {
        Write-Warning "[setup] refusing to remove a path outside the mirror: $skillFull"
        continue
      }
      if (Test-Path -LiteralPath $skillPath) {
        $item = Get-Item -LiteralPath $skillPath -Force
        if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
          Write-Warning "[setup] skipping reparse point: $skillPath"
          continue
        }
        Remove-Item -LiteralPath $skillPath -Recurse -Force -ErrorAction SilentlyContinue
      }
    }
  }
  Write-Host "[setup] core profile: explicit-only evaluation skills excluded from mirrors (the plugin still ships all skills)"
}

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\gen-mcp-manifest.ps1") -Root $root
if ($LASTEXITCODE -ne 0) { Fail "[setup] manifest generation failed" }

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
  if ($LASTEXITCODE -ne 0) { Fail "[setup] docker compose build failed" }
  Write-Host "[setup] starting infrastructure (postgres, redis, minio)"
  docker compose -f (Join-Path $root "docker-compose.yml") up -d --wait postgres redis minio
  if ($LASTEXITCODE -ne 0) { Fail "[setup] docker compose up failed or services are not healthy" }
} else {
  Write-Warning "[setup] docker engine is not available; building local binaries instead"
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "[setup] go toolchain not found; install Go or start Docker, then re-run setup"
  }
  New-Item -ItemType Directory -Force -Path (Join-Path $root "bin") | Out-Null
  $exe = if ($env:OS -eq "Windows_NT") { ".exe" } else { "" }
  go build -o "bin/hackathonkit-mcp$exe" ./cmd/hackathonkit-mcp
  if ($LASTEXITCODE -ne 0) { Fail "[setup] go build hackathonkit-mcp failed" }
  go build -o "bin/hackathonkit-demo-api$exe" ./cmd/demo-api
  if ($LASTEXITCODE -ne 0) { Fail "[setup] go build demo-api failed" }
  go build -o "bin/hackathonkit-ctl$exe" ./cmd/hackathonkit-ctl
  if ($LASTEXITCODE -ne 0) { Fail "[setup] go build hackathonkit-ctl failed" }
  Write-Host "[setup] local binaries built in bin/"
}

$global:LASTEXITCODE = 0
& (Join-Path $root "scripts\write-install-state.ps1") -Root $root -Profile $Profile -Runtime $runtime
if ($LASTEXITCODE -ne 0) { Fail "[setup] write-install-state failed" }

$codex = Get-Command codex -ErrorAction SilentlyContinue
if ($codex) {
  Write-Host "[setup] registering repo marketplace and installing the Hackathon Agent Kit plugin"
  & codex plugin remove hackathonkit *> $null
  if ($LASTEXITCODE -ne 0) { Write-Warning "[setup] codex plugin remove reported exit $LASTEXITCODE (continuing)" }
  & codex plugin marketplace add $root *> $null
  $marketExit = $LASTEXITCODE
  if ($marketExit -ne 0) {
    Write-Warning "[setup] codex plugin marketplace add failed (exit $marketExit)"
  }
  & codex plugin add hackathonkit@hackathonkit-local *> $null
  $addExit = $LASTEXITCODE
  if ($addExit -eq 0) {
    Write-Host "[setup] plugin installed; start a NEW Codex session to load it"
  } else {
    Write-Warning "[setup] could not install the plugin automatically (marketplace exit $marketExit, add exit $addExit). Run manually: codex plugin marketplace add '$root'; codex plugin add hackathonkit@hackathonkit-local"
  }
} else {
  Write-Warning "[setup] codex CLI not found; install it (npm install -g @openai/codex) and re-run setup"
}

Write-Host "[setup] done."
Write-Host "Next: open Codex from the repository root and start a NEW Codex session."
