param(
  [switch]$Yes
)
$ErrorActionPreference = "Stop"
if (-not $PSCommandPath) {
  Write-Error "[uninstall] PSCommandPath is empty; run this script as a file, not dot-sourced"
  exit 1
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root -or -not (Test-Path -LiteralPath (Join-Path $root "docker-compose.yml")) -or -not (Test-Path -LiteralPath (Join-Path $root "go.mod"))) {
  Write-Error "[uninstall] refusing: '$root' does not look like the hackathonkit repository root"
  exit 1
}

$codexFail = 0
if (Get-Command codex -ErrorAction SilentlyContinue) {
  if ($Yes) {
    $global:LASTEXITCODE = 0
    & codex plugin remove hackathonkit *> $null
    if ($LASTEXITCODE -ne 0) { Write-Warning "[uninstall] codex plugin remove failed"; $codexFail = 1 }
    $global:LASTEXITCODE = 0
    & codex plugin marketplace remove hackathonkit-local *> $null
    if ($LASTEXITCODE -ne 0) { Write-Warning "[uninstall] codex marketplace remove failed"; $codexFail = 1 }
    Write-Host "[uninstall] removed hackathonkit plugin and marketplace"
  } else {
    Write-Host "would run: codex plugin remove hackathonkit"
    Write-Host "would run: codex plugin marketplace remove hackathonkit-local"
  }
}

$targets = @(
  "plugins\hackathonkit\.mcp.json",
  ".agents\skills",
  ".claude\skills",
  "configs\install-state.json",
  "data\control",
  "bin"
)
$Failures = 0
foreach ($rel in $targets) {
  $target = Join-Path $root $rel
  $rootPrefix = $root.TrimEnd([System.IO.Path]::DirectorySeparatorChar, '/', '\') + [System.IO.Path]::DirectorySeparatorChar
  if (-not $target.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    Write-Warning "[uninstall] refusing to remove a path outside the repository: $target"
    $Failures++
    continue
  }
  if ($rel -in @(".agents\skills", ".claude\skills")) {
    # Shared skill directories: remove only the skills this plugin ships so
    # user-authored skills survive.
    $src = Join-Path $root "plugins\hackathonkit\skills"
    if (-not (Test-Path -LiteralPath $src)) { continue }
    foreach ($skill in Get-ChildItem -LiteralPath $src -Directory) {
      $skillTarget = Join-Path $target $skill.Name
      if ($Yes) {
        try { if (Test-Path -LiteralPath $skillTarget) { Remove-Item -LiteralPath $skillTarget -Recurse -Force -ErrorAction Stop } }
        catch { Write-Warning "[uninstall] failed to remove ${rel}\$($skill.Name): $($_.Exception.Message)"; $Failures++ }
      } else {
        Write-Host "would remove: $rel\$($skill.Name)"
      }
    }
    continue
  }
  if ($Yes) {
    try {
      if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction Stop }
    } catch {
      Write-Warning "[uninstall] failed to remove ${rel}: $($_.Exception.Message)"
      $Failures++
    }
  } else {
    Write-Host "would remove: $rel"
  }
}

if (-not $Yes) {
  Write-Host "[uninstall] dry run: re-run with -Yes to apply"
  exit 0
}
if ($Failures -gt 0 -or $codexFail -ne 0) {
  Write-Error "[uninstall] finished with warnings ($Failures path failure(s))"
  exit 1
}
Write-Host "[uninstall] done. data/input, data/artifacts, logs/ and committed files were kept (data/control was removed)."
