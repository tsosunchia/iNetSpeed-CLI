$ErrorActionPreference = "Stop"

$Repo = if ($env:REPO) { $env:REPO } else { "tsosunchia/iNetSpeed-CLI" }
$Binary = if ($env:BINARY) { $env:BINARY } else { "speedtest.exe" }
$ReleaseBase = if ($env:RELEASE_BASE) { $env:RELEASE_BASE } else { "https://github.com/$Repo/releases/latest/download" }

function Write-Step {
  param([string]$Message)
  Write-Host "==> $Message" -ForegroundColor Cyan
}

function Get-Architecture {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { return "amd64" }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
  }
}

function Test-IsAdministrator {
  $CurrentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $Principal = New-Object Security.Principal.WindowsPrincipal($CurrentIdentity)
  return $Principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Add-ToPath {
  param(
    [string]$Dir,
    [ValidateSet("User", "Machine")]
    [string]$Scope
  )

  $Current = [Environment]::GetEnvironmentVariable("Path", $Scope)
  $Parts = @()
  if ($Current) {
    $Parts = $Current -split ';' | Where-Object { $_ }
  }
  if ($Parts -contains $Dir) {
    return $false
  }

  $NewValue = if ([string]::IsNullOrWhiteSpace($Current)) { $Dir } else { "$Current;$Dir" }
  [Environment]::SetEnvironmentVariable("Path", $NewValue, $Scope)
  if (-not (($env:Path -split ';') -contains $Dir)) {
    $env:Path = if ([string]::IsNullOrWhiteSpace($env:Path)) { $Dir } else { "$env:Path;$Dir" }
  }
  return $true
}

$Arch = Get-Architecture
$IsAdmin = Test-IsAdministrator
$Asset = "speedtest-windows-$Arch.zip"
$TempDir = Join-Path ([IO.Path]::GetTempPath()) ("speedtest-" + [guid]::NewGuid().ToString("N"))
$ArchivePath = Join-Path $TempDir $Asset
$ChecksumPath = Join-Path $TempDir "checksums-sha256.txt"
$InstallDir = if ($env:INSTALL_DIR) {
  $env:INSTALL_DIR
} elseif ($IsAdmin) {
  Join-Path $env:ProgramFiles "speedtest"
} else {
  Join-Path $env:LOCALAPPDATA "Programs\speedtest"
}
$Target = Join-Path $InstallDir $Binary

New-Item -ItemType Directory -Path $TempDir | Out-Null
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

try {
  Write-Step "Downloading $Asset"
  Invoke-WebRequest -Uri "$ReleaseBase/$Asset" -OutFile $ArchivePath

  Write-Step "Downloading checksums-sha256.txt"
  Invoke-WebRequest -Uri "$ReleaseBase/checksums-sha256.txt" -OutFile $ChecksumPath

  Write-Step "Verifying checksum"
  $Expected = (Get-Content $ChecksumPath | Where-Object { $_ -match "\s+$Asset$" } | Select-Object -First 1).Split()[0]
  if (-not $Expected) {
    throw "Checksum for $Asset not found."
  }
  $Actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($Actual -ne $Expected.ToLowerInvariant()) {
    throw "Checksum mismatch for $Asset."
  }

  Write-Step "Extracting archive"
  Expand-Archive -Path $ArchivePath -DestinationPath $TempDir -Force
  Copy-Item -Path (Join-Path $TempDir $Binary) -Destination $Target -Force

  Write-Step "Installed to $Target"
  $PathScope = if ($IsAdmin) { "Machine" } else { "User" }
  if (Add-ToPath -Dir $InstallDir -Scope $PathScope) {
    $ScopeLabel = if ($PathScope -eq "Machine") { "machine" } else { "user" }
    Write-Warning "Added $InstallDir to the $ScopeLabel PATH. Open a new shell to use it."
  }

  & $Target --version
} finally {
  Remove-Item -Recurse -Force $TempDir
}
