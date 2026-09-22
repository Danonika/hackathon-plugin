param(
  [switch]$Yes
)
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

# Compare two trees by relative file path and content hash so a user-authored
# skill that merely shares a folder name with a shipped skill is not deleted,
# and a same-size edit is not mistaken for an unmodified copy.
function Test-SameTree([string]$A, [string]$B) {
  if (-not (Test-Path -LiteralPath $A) -or -not (Test-Path -LiteralPath $B)) { return $false }
  $aFiles = @(Get-ChildItem -LiteralPath $A -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($A.Length).TrimStart('\')
      "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
    } | Sort-Object)
  $bFiles = @(Get-ChildItem -LiteralPath $B -Recurse -File -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($B.Length).TrimStart('\')
      "$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
    } | Sort-Object)
  return @(Compare-Object -ReferenceObject $aFiles -DifferenceObject $bFiles).Count -eq 0
}

if (-not $PSCommandPath) {
  Fail "[uninstall] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
$markers = @("docker-compose.yml", "go.mod", "plugins\hackathonkit\.codex-plugin\plugin.json", "plugins\hackathonkit\skills")
$missingMarkers = @($markers | Where-Object { -not (Test-Path -LiteralPath (Join-Path $root $_)) })
if (-not $root -or $missingMarkers.Count -gt 0) {
  Fail "[uninstall] refusing: '$root' does not look like the hackathonkit repository root (missing: $($missingMarkers -join ', '))"
}

$codexFail = 0
if (Get-Command codex -CommandType Application -ErrorAction SilentlyContinue) {
  if ($Yes) {
    $global:LASTEXITCODE = 0
    & codex plugin remove hackathonkit *> $null
    $removeExit = $LASTEXITCODE
    if ($removeExit -ne 0) { Write-Warning "[uninstall] codex plugin remove failed (exit $removeExit)"; $codexFail = 1 }
    $global:LASTEXITCODE = 0
    & codex plugin marketplace remove hackathonkit-local *> $null
    $marketExit = $LASTEXITCODE
    if ($marketExit -ne 0) { Write-Warning "[uninstall] codex marketplace remove failed (exit $marketExit)"; $codexFail = 1 }
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
# Remove the /verify custom prompt this installer placed for Codex.
if ($Yes) {
  & (Join-Path $root "scripts\install-prompts.ps1") remove | Out-Null
} else {
  Write-Host "would remove: $HOME\.codex\prompts\*.md (unmodified prompts only)"
}

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
      if (-not (Test-Path -LiteralPath $skillTarget)) { continue }
      $item = Get-Item -LiteralPath $skillTarget -Force
      if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
        Write-Warning "[uninstall] skipping reparse point: $rel\$($skill.Name)"
        continue
      }
      if (-not (Test-SameTree $skill.FullName $skillTarget)) {
        Write-Warning "[uninstall] keeping $rel\$($skill.Name): it differs from the shipped skill (user-authored?)"
        continue
      }
      if ($Yes) {
        try { Remove-Item -LiteralPath $skillTarget -Recurse -Force -ErrorAction Stop }
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
  Fail "[uninstall] finished with warnings ($Failures path failure(s))"
}
Write-Host "[uninstall] done. data/input, data/artifacts, logs/ and committed files were kept (data/control was removed)."
