$ErrorActionPreference = "Stop"
if (-not $PSCommandPath) {
  Write-Error "[repair] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml"))) {
  Write-Error "[repair] refusing: '$root' does not look like the hackathonkit repository root"
  exit 1
}
Set-Location -LiteralPath $root -ErrorAction Stop
Write-Host "[repair] repository root: $root"

function Invoke-RequiredScript([string]$Path, [hashtable]$Parameters) {
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "[repair] missing helper: $Path" }
  $global:LASTEXITCODE = 0
  & $Path @Parameters 2>&1 | ForEach-Object {
    if ($_ -is [System.Management.Automation.ErrorRecord]) { throw $_ }
    Write-Output $_
  }
  if ($LASTEXITCODE -ne 0) { throw "[repair] $Path failed (exit $LASTEXITCODE)" }
}
. (Join-Path $PSScriptRoot "skill-profile.ps1")
$installProfile = Get-InstalledSkillProfile $root
Invoke-RequiredScript (Join-Path $PSScriptRoot "sync-harness.ps1") @{InstallProfile = $installProfile}
Invoke-RequiredScript (Join-Path $PSScriptRoot "gen-mcp-manifest.ps1") @{Root = $root}
Invoke-RequiredScript (Join-Path $PSScriptRoot "install-prompts.ps1") @{Action = "install"}
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

$runtime = "binary"
. (Join-Path $PSScriptRoot "native-probe.ps1")
$docker = Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($docker) {
  try {
    $probe = Invoke-NativeProbe $docker.Source @("info") "" $root 5000
    if ($probe.Code -eq 0) { $runtime = "docker" }
  } catch { Write-Warning "[repair] Docker unavailable: $_" }
}

$bin = Join-Path $root "bin\hackathonkit-mcp.exe"
$demo = Join-Path $root "bin\hackathonkit-demo-api.exe"
$ctl = Join-Path $root "bin\hackathonkit-ctl.exe"
if ((-not (Test-Path -LiteralPath $bin)) -or (-not (Test-Path -LiteralPath $demo)) -or (-not (Test-Path -LiteralPath $ctl))) {
  if ($runtime -eq "binary") {
    if (Get-Command go -CommandType Application -ErrorAction SilentlyContinue) {
      $binDir = Join-Path $root "bin"
      New-Item -ItemType Directory -Force -Path $binDir -ErrorAction Stop | Out-Null
      go build -trimpath -buildvcs=false -o (Join-Path $binDir "hackathonkit-mcp.exe") ./cmd/hackathonkit-mcp
      if ($LASTEXITCODE -ne 0) { Write-Error "[repair] go build hackathonkit-mcp failed"; exit 1 }
      go build -trimpath -buildvcs=false -o (Join-Path $binDir "hackathonkit-demo-api.exe") ./cmd/demo-api
      if ($LASTEXITCODE -ne 0) { Write-Error "[repair] go build demo-api failed"; exit 1 }
      go build -trimpath -buildvcs=false -o (Join-Path $binDir "hackathonkit-ctl.exe") ./cmd/hackathonkit-ctl
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

$stateScript = Join-Path $root "scripts\write-install-state.ps1"
if (-not (Test-Path -LiteralPath $stateScript)) {
  Write-Error "[repair] write-install-state.ps1 is missing"
  exit 1
}
Invoke-RequiredScript $stateScript @{Root = $root; Profile = $installProfile; Runtime = $runtime}

if (Get-Command codex -CommandType Application -ErrorAction SilentlyContinue) {
  $global:LASTEXITCODE = 0
  & codex plugin marketplace add $root *> $null
  $marketExit = $LASTEXITCODE
  if ($marketExit -ne 0) { throw "[repair] codex plugin marketplace add failed (exit $marketExit)" }
  $global:LASTEXITCODE = 0
  & codex plugin add hackathonkit@hackathonkit-local *> $null
  $addExit = $LASTEXITCODE
  if ($addExit -eq 0) {
    Write-Host "[repair] plugin reinstalled; start a NEW Codex session"
  } else {
    throw "[repair] could not reinstall the plugin automatically (marketplace exit $marketExit, add exit $addExit)"
  }
}

Write-Host "[repair] done. Run scripts/doctor.ps1 to verify."
