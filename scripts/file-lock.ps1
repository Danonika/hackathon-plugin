# Hold an exclusive OS file handle for the transaction. Closing the handle or
# process exit releases it. The inode must stay in place while other runs wait.
function Enter-MirrorLock([string]$Root, [string]$Name) {
  $dir = [System.IO.Directory]::CreateDirectory((Join-Path $Root "data/locks"))
  $path = Join-Path $dir.FullName $Name
  $deadline = [DateTime]::UtcNow.AddSeconds(10)
  while ($true) {
    try {
      return [IO.File]::Open($path, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    } catch [IO.IOException] {
      if ([DateTime]::UtcNow -ge $deadline) { throw }
      Start-Sleep -Milliseconds 200
    }
  }
}
