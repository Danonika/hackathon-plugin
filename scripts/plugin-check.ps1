$ErrorActionPreference = "Continue"

# Resolve symlinks/junctions to their final target. Resolve-Path only follows
# links on PowerShell 6+; on Windows PowerShell 5.1 it returns the link path, so
# a symlink inside the plugin pointing outside it could pass a containment
# check. Walk reparse points explicitly instead.
function Resolve-LinkTarget([string]$Path) {
  $full = [System.IO.Path]::GetFullPath($Path)
  for ($i = 0; $i -lt 40; $i++) {
    $item = Get-Item -LiteralPath $full -Force -ErrorAction Stop
    if (-not ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint)) { return $item.FullName }
    $target = $item.Target
    if ($target -is [System.Array]) { $target = $target[0] }
    if (-not $target) { throw "cannot resolve link target for $full" }
    if ([System.IO.Path]::IsPathRooted($target)) { $full = $target }
    else { $full = Join-Path (Split-Path -Parent $full) $target }
  }
  throw "too many reparse points resolving $Path"
}

$codexCmd = Get-Command codex -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $codexCmd) {
  Write-Host "[plugin-check] codex executable not found on PATH"
  exit 1
}
$json = (& $codexCmd.Source plugin list --json 2>$null) -join "`n"
$codexExit = $LASTEXITCODE
if ($codexExit -ne 0 -or -not $json) {
  Write-Host "[plugin-check] codex plugin list is unavailable (exit $codexExit)"
  exit 1
}
try {
  $parsed = $json | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse codex plugin list output: $($_.Exception.Message)"
  exit 1
}
if (-not ($parsed.PSObject.Properties.Name -contains 'installed')) {
  Write-Host "[plugin-check] unexpected codex plugin list shape (no installed array)"
  exit 1
}
if ($null -eq $parsed.installed) {
  Write-Host "[plugin-check] unexpected codex plugin list shape (installed is null)"
  exit 1
}
$installed = if ($parsed.installed -is [System.Array]) { $parsed.installed } else { @($parsed.installed) }
if ($installed.Count -eq 0) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$entry = @($installed) | Where-Object { $_.name -eq "hackathonkit" } | Select-Object -First 1
if (-not $entry) {
  Write-Host "[plugin-check] hackathonkit plugin is not installed"
  exit 1
}
$sourceProperty = $entry.PSObject.Properties['source']
if (-not $sourceProperty -or -not $sourceProperty.Value -or -not ($sourceProperty.Value.PSObject.Properties.Name -contains 'path')) {
  Write-Host "[plugin-check] installed plugin entry has an unexpected shape (missing source.path); re-run scripts/setup.ps1"
  exit 1
}
$sourcePath = $sourceProperty.Value.path
if (-not $sourcePath) {
  Write-Host "[plugin-check] installed plugin has no source.path; re-run scripts/setup.ps1"
  exit 1
}
$mcpPath = Join-Path $sourcePath ".mcp.json"
if (-not (Test-Path -LiteralPath $mcpPath)) {
  Write-Host "[plugin-check] .mcp.json is missing in the installed plugin; run scripts/setup.ps1"
  exit 1
}
try {
  $mcp = Get-Content -LiteralPath $mcpPath -Raw -Encoding UTF8 | ConvertFrom-Json
} catch {
  Write-Host "[plugin-check] cannot parse .mcp.json: $($_.Exception.Message)"
  exit 1
}
$server = $mcp.mcpServers.hackathonkit
if (-not $server -or -not $server.command) {
  Write-Host "[plugin-check] hackathonkit server is missing in .mcp.json"
  exit 1
}
$cmd = [string]$server.command
$argList = @($server.args | ForEach-Object { [string]$_ })
$isNodeLauncher = $cmd -ceq "node"
if ($isNodeLauncher) {
  if ($argList.Count -lt 1 -or [IO.Path]::GetFileName($argList[0]) -cne "hackathonkit-mcp-launcher.js") {
    throw "[plugin-check] expected the Hackathon Agent Kit Node launcher as the first argument"
  }
  $cmd = $argList[0]
}
try {
  if (-not [System.IO.Path]::IsPathRooted($cmd)) {
    $cmd = Join-Path $sourcePath $cmd
  }
  $cmdFull = [System.IO.Path]::GetFullPath($cmd)
  $sourceFullRoot = [System.IO.Path]::GetFullPath($sourcePath)
} catch {
  Write-Host "[plugin-check] cannot resolve the installed plugin command path: $($_.Exception.Message)"
  exit 1
}
if (-not (Test-Path -LiteralPath $cmdFull -PathType Leaf)) {
  Write-Host "[plugin-check] MCP command does not exist: $cmdFull"
  exit 1
}
# Resolve symlinks/junctions so a link inside the plugin cannot point outside
# it. A resolution failure (broken or replaced link, race) is a clean
# diagnostic and exit 1, not an unhandled error.
try {
  $cmdResolved = Resolve-LinkTarget $cmdFull
  $sourceResolved = Resolve-LinkTarget $sourceFullRoot
} catch {
  Write-Host "[plugin-check] cannot resolve the installed plugin command path: $($_.Exception.Message)"
  exit 1
}
$comparison = if ($env:OS -eq "Windows_NT") { [System.StringComparison]::OrdinalIgnoreCase } else { [System.StringComparison]::Ordinal }
$sourcePrefix = $sourceResolved.TrimEnd([System.IO.Path]::DirectorySeparatorChar, '/', '\') + [System.IO.Path]::DirectorySeparatorChar
if (-not $cmdResolved.StartsWith($sourcePrefix, $comparison)) {
  Write-Host "[plugin-check] refusing to execute a command outside the installed plugin: $cmdResolved"
  exit 1
}
if ($isNodeLauncher) {
  $node = Get-Command node -CommandType Application -ErrorAction Stop | Select-Object -First 1
  $argList[0] = $cmdResolved
  $cmd = $node.Source
} else { $cmd = $cmdResolved }

$childEnv = @{}
$supportedEnv = @(
  "OPENAI_API_KEY","OPENAI_BASE_URL","OPENAI_TIMEOUT_SEC","OPENAI_MODEL",
  "NVIDIA_API_KEY","NVIDIA_BASE_URL","POSTGRES_DSN","MINIO_ENDPOINT",
  "MINIO_ACCESS_KEY","MINIO_SECRET_KEY","MINIO_BUCKET","MINIO_USE_SSL",
  "TELEGRAM_BOT_TOKEN","TELEGRAM_CHAT_ID","TELEGRAM_ALLOWED_CHAT_IDS",
  "TELEGRAM_API_BASE","TELEGRAM_TIMEOUT_SEC","HACKATHONKIT_BACKEND_TIMEOUT_SEC"
)
if ($null -ne $server.env) {
  if ($server.env -isnot [PSCustomObject]) { throw "[plugin-check] expected an object for manifest env" }
  foreach ($property in $server.env.PSObject.Properties) {
    if ($property.Name -cnotin $supportedEnv -or $property.Value -isnot [string]) {
      throw "[plugin-check] unsupported manifest env setting or value"
    }
    $childEnv[$property.Name] = $property.Value
  }
}

$temp = Join-Path ([IO.Path]::GetTempPath()) ("hackathonkit-plugin-check-" + [guid]::NewGuid().ToString("N"))
try {
  New-Item -ItemType Directory -Path $temp -ErrorAction Stop | Out-Null
} catch {
  Write-Host "[plugin-check] cannot create the temp directory ${temp}: $($_.Exception.Message)"
  exit 1
}
$requests = @(
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"plugin-check","version":"0"}}}',
  '{"jsonrpc":"2.0","method":"notifications/initialized"}',
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}'
) -join "`n"

. (Join-Path $PSScriptRoot "native-probe.ps1")
try {
  $probe = Invoke-NativeProbe $cmd $argList ($requests + "`n") $temp 120000 $childEnv
  if ($probe.Code -ne 0) { throw "MCP exited with code $($probe.Code)" }
  $out = $probe.Out -split '\r?\n'
} catch {
  Write-Host "[plugin-check] MCP verification failed: $_"
  exit 1
} finally { Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue }

$responses = @{}
foreach ($line in @($out | Where-Object { $_ -and $_.Trim() -ne "" })) {
  # Frame strictly by line: drop progress/log noise and partial JSON instead of
  # letting it poison every later response.
  $obj = $null
  try { $obj = $line | ConvertFrom-Json } catch { $obj = $null }
  if ($null -eq $obj) { continue }
  if ($obj.id -in @(1, 2, 3)) {
    if ($responses.ContainsKey([string]$obj.id)) { throw "[plugin-check] duplicate response id $($obj.id)" }
    $responses[[string]$obj.id] = $obj
    if ($obj.error) {
      Write-Host "[plugin-check] FAIL: error response for id=$($obj.id): $($obj.error.message)"
      exit 1
    }
  }
}
foreach ($want in @(1, 2, 3)) {
  $obj = $responses[[string]$want]
  if (-not $obj) {
    Write-Host "[plugin-check] FAIL: missing JSON-RPC response for id=$want from a foreign working directory"
    exit 1
  }
  $hasResult = $obj.PSObject.Properties.Name -contains "result"
  if (-not $hasResult -or $null -eq $obj.result -or ($want -eq 2 -and $obj.result.tools -isnot [array])) {
    Write-Host "[plugin-check] FAIL: response for id=$want has no result"
    exit 1
  }
  if ($obj.result.isError) {
    Write-Host "[plugin-check] FAIL: tool call id=$want reported isError"
    exit 1
  }
}
Write-Host "[plugin-check] ok: installed plugin starts the MCP from a foreign working directory"
exit 0
