function Get-InstalledSkillProfile([string]$Root) {
  $state = Join-Path $Root "configs/install-state.json"
  if (-not [IO.File]::Exists($state)) { return "full" }
  try {
    $record = Get-Content -LiteralPath $state -Raw -Encoding UTF8 -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    if ($record.profile -is [string] -and $record.profile -in @("core", "full")) { return $record.profile.ToLowerInvariant() }
  } catch { }
  Write-Warning "[skills] could not read the install profile; using full"
  return "full"
}

function Test-SkillInProfile([string]$Directory, [string]$InstallProfile) {
  if ($InstallProfile -ne "core") { return $true }
  $policy = Join-Path $Directory "agents/openai.yaml"
  if (-not [IO.File]::Exists($policy)) { return $true }
  return -not (Select-String -LiteralPath $policy -Pattern '^\s+allow_implicit_invocation:\s*false(\s*(#.*)?)?$' -CaseSensitive -Quiet -ErrorAction Stop)
}

function Select-StagedSkillProfile([string]$Stage, [string]$InstallProfile) {
  if ($InstallProfile -eq "full") { return }
  foreach ($skill in Get-ChildItem -LiteralPath $Stage -Directory -Force) {
    if (-not (Test-SkillInProfile $skill.FullName $InstallProfile)) {
      Remove-Item -LiteralPath $skill.FullName -Recurse -Force -ErrorAction Stop
    }
  }
}
