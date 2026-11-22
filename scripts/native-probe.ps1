# Bounded native process with separate stdout/stderr and no command shell.
function Invoke-NativeProbe([string]$Exe, [string[]]$Arguments, [string]$InputText, [string]$WorkingDirectory, [int]$TimeoutMs, [hashtable]$Environment = @{}) {
  $start = New-Object Diagnostics.ProcessStartInfo
  $start.FileName = $Exe
  $start.WorkingDirectory = $WorkingDirectory
  $start.UseShellExecute = $false
  foreach ($name in $Environment.Keys) { $start.EnvironmentVariables[$name] = $Environment[$name] }
  $start.CreateNoWindow = $true
  $start.RedirectStandardInput = $true
  $start.RedirectStandardOutput = $true
  $start.RedirectStandardError = $true
  if ($start.PSObject.Properties["ArgumentList"]) {
    foreach ($argument in $Arguments) { $start.ArgumentList.Add($argument) }
  } else {
    # Windows PowerShell 5.1 only accepts a CreateProcess command line.
    $quoted = foreach ($argument in $Arguments) {
      '"' + [regex]::Replace(([regex]::Replace($argument, '(\\*)"', '$1$1\"')), '(\\+)$', '$1$1') + '"'
    }
    $start.Arguments = $quoted -join " "
  }
  $proc = New-Object Diagnostics.Process
  $proc.StartInfo = $start
  $started = $false
  try {
    $started = $proc.Start()
    if (-not $started) { throw "could not start $Exe" }
    $stdout = $proc.StandardOutput.ReadToEndAsync()
    $stderr = $proc.StandardError.ReadToEndAsync()
    $proc.StandardInput.Write($InputText)
    $proc.StandardInput.Close()
    if (-not $proc.WaitForExit($TimeoutMs)) { throw "$Exe exceeded timeout of $TimeoutMs ms" }
    if (-not $stdout.Wait(3000) -or -not $stderr.Wait(3000)) { throw "$Exe output pipes did not close" }
    return [PSCustomObject]@{Code=$proc.ExitCode; Out=$stdout.Result; Err=$stderr.Result}
  } finally {
    if ($started -and -not $proc.HasExited) {
      try { $proc.Kill($true) }
      catch {
        if ($env:OS -eq "Windows_NT") {
          & (Join-Path $env:SystemRoot "System32/taskkill.exe") /PID $proc.Id /T /F *> $null
        }
        if (-not $proc.HasExited) { $proc.Kill() }
      }
    }
    $proc.Dispose()
  }
}
