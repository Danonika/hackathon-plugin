param(
  [switch]$Yes
)
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Host $Message -ForegroundColor Red
  exit 1
}

# Compare two trees by relative directory/file entries and content hash so a
# user-authored skill that merely shares a folder name with a shipped skill is
# not deleted, and a same-size edit is not mistaken for an unmodified copy.
function Get-TreeEntries([string]$Base) {
  @(Get-ChildItem -LiteralPath $Base -Recurse -Force |
    Where-Object { -not ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) } |
    ForEach-Object {
      $rel = $_.FullName.Substring($Base.Length).TrimStart('\', '/')
      if ($_.PSIsContainer) { "d|$rel" }
      else { "f|$rel|$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)" }
    } | Sort-Object)
}

function Test-SameTree([string]$A, [string]$B) {
  if (-not (Test-Path -LiteralPath $A) -or -not (Test-Path -LiteralPath $B)) { return $false }
  # Directory entries are included so two trees differing only in empty
  # subdirectories do not compare equal.
  return @(Compare-Object -ReferenceObject (Get-TreeEntries $A) -DifferenceObject (Get-TreeEntries $B)).Count -eq 0
}

# Recursively delete a tree without following junctions/symlinks.
function Remove-TreeNoReparse([string]$Path) {
  $item = Get-Item -LiteralPath $Path -Force
  if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
    Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
    return
  }
  if ($item.PSIsContainer) {
    foreach ($child in Get-ChildItem -LiteralPath $Path -Force) {
      Remove-TreeNoReparse $child.FullName
    }
  }
  Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
}

if (-not $PSCommandPath) {
  Fail "[uninstall] PSCommandPath is empty; run this script as a file, not dot-sourced"
}
$root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if (-not $root) {
  Fail "[uninstall] cannot resolve the repository root from '$PSCommandPath'"
}
$markers = @("docker-compose.yml", "go.mod", "plugins/hackathonkit/.codex-plugin/plugin.json", "plugins/hackathonkit/skills")
$missingMarkers = @($markers | Where-Object { -not (Test-Path -LiteralPath (Join-Path $root $_)) })
if ($missingMarkers.Count -gt 0) {
  Fail "[uninstall] refusing: '$root' does not look like the hackathonkit repository root (missing: $($missingMarkers -join ', '))"
}

$codexFail = 0
$codex = Get-Command codex -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($codex) {
  foreach ($kind in @("plugin", "marketplace")) {
    if ($kind -eq "plugin") {
      $listArgs = @("plugin", "list", "--json")
      $removeArgs = @("plugin", "remove", "hackathonkit")
      $name = "hackathonkit"
      $field = "installed"
    } else {
      $listArgs = @("plugin", "marketplace", "list", "--json")
      $removeArgs = @("plugin", "marketplace", "remove", "hackathonkit-local")
      $name = "hackathonkit-local"
      $field = "marketplaces"
    }
    $listing = & $codex.Source @listArgs 2>$null
    if ($LASTEXITCODE -ne 0) {
      Write-Warning "[uninstall] could not enumerate $kind registrations"
      $codexFail = 1
      continue
    }
    try {
      $values = (($listing -join [Environment]::NewLine) | ConvertFrom-Json).$field
      if ($values -isnot [array]) { throw "missing $field array" }
      $registered = @($values | Where-Object { $_.name -ceq $name }).Count -gt 0
    } catch { Write-Warning "[uninstall] invalid $kind listing"; $codexFail = 1; continue }
    if (-not $registered) { continue }
    if ($Yes) {
      & $codex.Source @removeArgs *> $null
      if ($LASTEXITCODE -ne 0) { Write-Warning "[uninstall] failed removing $name"; $codexFail = 1 }
    } else { Write-Host "would run: codex $($removeArgs -join ' ')" }
  }
}

$targets = @(
  "plugins/hackathonkit/.mcp.json",
  ".agents/skills",
  ".claude/skills",
  "configs/install-state.json",
  "data/control",
  "bin"
)
# Remove the /verify custom prompt this installer placed for Codex.
$Failures = 0
if ($Yes) {
  $promptsScript = Join-Path $root "scripts\install-prompts.ps1"
  if (-not (Test-Path -LiteralPath $promptsScript)) {
    Write-Warning "[uninstall] install-prompts.ps1 is missing; cannot remove the Codex prompts"
  } else {
    try {
      & $promptsScript remove | Out-Null
      # A child `exit 1` does not throw; it only sets $LASTEXITCODE.
      if ($LASTEXITCODE -ne 0) {
        Write-Warning "[uninstall] prompt removal reported a failure (exit $LASTEXITCODE)"
        $Failures++
      }
    } catch {
      Write-Warning "[uninstall] prompt removal failed: $($_.Exception.Message)"
      $Failures++
    }
  }
} else {
  Write-Host "would remove: $HOME\.codex\prompts\*.md (unmodified prompts only)"
}

foreach ($rel in $targets) {
  $target = Join-Path $root $rel
  $rootPrefix = $root.TrimEnd([System.IO.Path]::DirectorySeparatorChar, '/', '\') + [System.IO.Path]::DirectorySeparatorChar
  $intermediateLink = $false
  $ancestor = Split-Path -Parent $target
  while ($ancestor -and $ancestor -ne $root) {
    if ([IO.Directory]::Exists($ancestor) -and ([IO.File]::GetAttributes($ancestor) -band [IO.FileAttributes]::ReparsePoint)) {
      $intermediateLink = $true
      break
    }
    $ancestor = Split-Path -Parent $ancestor
  }
  if ($intermediateLink -or -not $target.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    Write-Warning "[uninstall] refusing to remove a path outside the repository: $target"
    $Failures++
    continue
  }
  if ($rel -in @(".agents/skills", ".claude/skills")) {
    # Shared skill directories: remove only the skills this plugin ships so
    # user-authored skills survive.
    $src = Join-Path $root "plugins/hackathonkit/skills"
    if (-not (Test-Path -LiteralPath $src)) { continue }
    foreach ($skill in Get-ChildItem -LiteralPath $src -Directory -Force) {
      $skillTarget = Join-Path $target $skill.Name
      if (-not (Test-Path -LiteralPath $skillTarget)) { continue }
      $item = Get-Item -LiteralPath $skillTarget -Force
      if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
        Write-Warning "[uninstall] skipping reparse point: $rel\$($skill.Name)"
        $Failures++
        continue
      }
      $same = $false
      try {
        $same = Test-SameTree $skill.FullName $skillTarget
      } catch {
        # Get-TreeEntries walks and hashes both trees; an unreadable or locked
        # file under $ErrorActionPreference="Stop" must be recorded, not abort
        # the whole uninstall mid-way.
        Write-Warning "[uninstall] could not compare ${rel}\$($skill.Name): $($_.Exception.Message)"
        $Failures++
        continue
      }
      if (-not $same) {
        Write-Warning "[uninstall] keeping $rel\$($skill.Name): it differs from the shipped skill (user-authored?)"
        continue
      }
      if ($Yes) {
        try {
          # Test-SameTree hashed the tree above; re-fetch and re-validate
          # immediately before deleting so a path swapped for a reparse point in
          # between cannot defeat the junction/user-skill protection.
          $final = Get-Item -LiteralPath $skillTarget -Force -ErrorAction Stop
          if ($final.Attributes -band [System.IO.FileAttributes]::ReparsePoint -or -not (Test-SameTree $skill.FullName $final.FullName)) {
            Write-Warning "[uninstall] $rel\$($skill.Name) changed before removal; skipping"
            $Failures++
            continue
          }
          Remove-TreeNoReparse $final.FullName
        }
        catch { Write-Warning "[uninstall] failed to remove ${rel}\$($skill.Name): $($_.Exception.Message)"; $Failures++ }
      } else {
        Write-Host "would remove: $rel\$($skill.Name)"
      }
    }
    continue
  }
  if ($Yes) {
    try {
      if (Test-Path -LiteralPath $target) {
        $targetItem = Get-Item -LiteralPath $target -Force
        if ($targetItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
          Remove-Item -LiteralPath $target -Force -ErrorAction Stop
        } else {
          Remove-TreeNoReparse $target
        }
      }
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
  Fail "[uninstall] partially completed ($Failures path failure(s), $codexFail Codex failure(s)); review the warnings above"
}
Write-Host "[uninstall] done. data/input, data/artifacts, logs/ and committed files were kept (data/control was removed)."
