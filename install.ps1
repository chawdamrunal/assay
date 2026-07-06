<#
.SYNOPSIS
  Assay installer for Windows (PowerShell). Mirrors install.sh.

.DESCRIPTION
  Usage:
    irm https://github.com/chawdamrunal/assay/install.ps1 | iex

  Override the version:
    $env:ASSAY_VERSION = 'v0.1.0'; irm https://github.com/chawdamrunal/assay/install.ps1 | iex

  Override the install directory:
    $env:ASSAY_INSTALL_DIR = 'C:\tools\assay'; irm https://github.com/chawdamrunal/assay/install.ps1 | iex

  Downloads the checksum-verified Windows release from GitHub, installs
  assay.exe to %LOCALAPPDATA%\Assay\bin, and adds that directory to your
  user PATH. No administrator rights required.
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 can default to TLS 1.0/1.1, which GitHub rejects.
try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 } catch {}

# ---- Configuration (env overrides, since a piped `iex` script takes no args) ----
$Repo       = if ($env:ASSAY_REPO)        { $env:ASSAY_REPO }        else { 'chawdamrunal/assay' }
$Version    = $env:ASSAY_VERSION
$InstallDir = $env:ASSAY_INSTALL_DIR

# ---- Helpers ----
# Fail via `throw` (not `exit`) so `irm | iex` reports the error without
# terminating the caller's PowerShell session.
function Write-Info([string] $Message) { Write-Host "assay-install: $Message" }
function Fail([string] $Message)        { throw "assay-install: error: $Message" }

# ---- Architecture detection ----
function Resolve-Arch {
  # PROCESSOR_ARCHITEW6432 is set when a 32-bit process runs on a 64-bit OS.
  $raw = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
  switch ($raw) {
    'AMD64' { return 'amd64' }
    'ARM64' {
      Write-Info 'Windows arm64 detected - installing the amd64 build (runs under emulation).'
      return 'amd64'
    }
    'x86'   { Fail '32-bit x86 is not supported - no 32-bit Assay build exists.' }
    default { Fail "unsupported architecture: $raw" }
  }
}

# ---- Latest version from the GitHub API ----
function Resolve-LatestVersion {
  try {
    $release = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$Repo/releases/latest"
    return $release.tag_name
  } catch {
    Fail "could not determine latest version (set `$env:ASSAY_VERSION=v0.X.Y to override). $($_.Exception.Message)"
  }
}

# ---- Main ----
$arch = Resolve-Arch

if ([string]::IsNullOrEmpty($Version)) {
  $Version = Resolve-LatestVersion
}
if ([string]::IsNullOrEmpty($Version)) {
  Fail 'could not determine latest version (set $env:ASSAY_VERSION=v0.X.Y to override)'
}

# goreleaser strips the leading 'v' for archive names and ships Windows as .zip.
$versionNum = $Version.TrimStart('v')
$zip        = "assay_${versionNum}_windows_${arch}.zip"
$base       = "https://github.com/$Repo/releases/download/$Version"
$zipUrl     = "$base/$zip"
$sumsUrl    = "$base/checksums.txt"

if ([string]::IsNullOrEmpty($InstallDir)) {
  $InstallDir = Join-Path $env:LOCALAPPDATA 'Assay\bin'
}

Write-Info "version: $Version"
Write-Info "platform: windows/$arch"
Write-Info "install dir: $InstallDir"

$tmp = Join-Path $env:TEMP ('assay-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $zipPath  = Join-Path $tmp $zip
  $sumsPath = Join-Path $tmp 'checksums.txt'

  Write-Info "downloading $zip"
  try { Invoke-WebRequest -UseBasicParsing -Uri $zipUrl -OutFile $zipPath }
  catch { Fail "download failed: $zipUrl ($($_.Exception.Message))" }

  Write-Info 'verifying checksum'
  try { Invoke-WebRequest -UseBasicParsing -Uri $sumsUrl -OutFile $sumsPath }
  catch { Fail "checksum file download failed: $sumsUrl" }

  $entry = Select-String -Path $sumsPath -Pattern ([regex]::Escape($zip)) | Select-Object -First 1
  if (-not $entry) { Fail "no checksum entry for $zip in checksums.txt" }
  $expected = (($entry.Line -split '\s+') | Where-Object { $_ -ne '' })[0].ToLower()
  $actual   = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLower()
  if ($expected -ne $actual) { Fail "checksum mismatch (expected $expected, got $actual)" }

  Write-Info 'extracting'
  Expand-Archive -Path $zipPath -DestinationPath $tmp -Force
  $exe = Join-Path $tmp 'assay.exe'
  if (-not (Test-Path $exe)) { Fail 'assay.exe not found in the downloaded archive' }

  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Copy-Item -Path $exe -Destination (Join-Path $InstallDir 'assay.exe') -Force
  Write-Info "installed: $(Join-Path $InstallDir 'assay.exe')"
}
finally {
  Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
}

# ---- PATH: add to the persistent user PATH (idempotent) + this session ----
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($null -eq $userPath) { $userPath = '' }
$onUserPath = ($userPath -split ';' | Where-Object { $_ -ne '' }) -contains $InstallDir
if (-not $onUserPath) {
  $trimmed = $userPath.TrimEnd(';')
  $newPath = if ($trimmed) { "$trimmed;$InstallDir" } else { $InstallDir }
  [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
  Write-Info "added $InstallDir to your user PATH - restart your terminal for it to take effect."
}
# Make `assay` resolvable in the current session immediately.
if (($env:Path -split ';') -notcontains $InstallDir) { $env:Path = "$env:Path;$InstallDir" }

Write-Host ''
Write-Host 'Next steps:'
Write-Host '  assay auth status       # see which credential method is active'
Write-Host '  assay inventory         # list installed plugins / MCP servers / hooks'
Write-Host '  assay scan <target>     # run a 5-stage security scan'
Write-Host ''
Write-Host "Read the threat model:  https://github.com/$Repo/blob/main/docs/threat-model-2026.md"
