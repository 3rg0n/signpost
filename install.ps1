<#
.SYNOPSIS
    Installs signpost from a tagged GitHub release.

.DESCRIPTION
    Downloads a release archive and verifies its SHA-256 against the checksums.txt
    published with that release before anything is unpacked. A script fetched from
    the network and run has no business installing a binary it did not check, and
    "the download completed" is not a check.

.EXAMPLE
    iex "& { $(irm https://raw.githubusercontent.com/3rg0n/signpost/main/install.ps1) }"

.EXAMPLE
    # Pinning a version, which iex cannot pass arguments to — download and run it:
    irm https://raw.githubusercontent.com/3rg0n/signpost/main/install.ps1 -OutFile install.ps1
    .\install.ps1 -Version v0.1.0
#>
[CmdletBinding()]
param(
    # The release to install. Defaults to the latest.
    [string]$Version = $env:SIGNPOST_VERSION,

    # Where to put the binary. Defaults to %LOCALAPPDATA%\signpost\bin.
    [string]$InstallDir = $env:SIGNPOST_INSTALL_DIR,

    # Skip adding InstallDir to the user PATH.
    [switch]$NoPath
)

# Stop on the first error rather than continuing past a failed download into an
# install of whatever is on disk.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo = '3rg0n/signpost'
$Bin = 'signpost.exe'

function Write-Step { param([string]$Message) Write-Host "==> $Message" }

function Get-Arch {
    # PROCESSOR_ARCHITECTURE reports the *process* architecture, which is x86 for a
    # 32-bit PowerShell on a 64-bit machine. RuntimeInformation reports the OS.
    switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
        'X64' { 'amd64' }
        'Arm64' { 'arm64' }
        default {
            throw "unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)"
        }
    }
}

function Get-LatestVersion {
    Write-Step 'resolving the latest release'
    # The redirect target of /releases/latest rather than the API: no rate limit a
    # shared CI address will hit, and no token needed.
    $resolved = $null
    try {
        $response = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" `
            -MaximumRedirection 0 -ErrorAction SilentlyContinue -UseBasicParsing
        $resolved = $response.Headers['Location']
    }
    catch {
        # PowerShell 5.1 throws on a 3xx when redirection is disabled; the response
        # is still attached to the exception.
        $resolved = $_.Exception.Response.Headers.Location
    }
    if (-not $resolved) {
        throw 'could not determine the latest version; pass -Version'
    }
    $tag = ([string]$resolved).Split('/')[-1]
    if ($tag -notmatch '^v') {
        throw "unexpected latest tag '$tag'; pass -Version"
    }
    $tag
}

# TLS 1.2 is not the default in PowerShell 5.1, and github.com does not serve
# anything older. Without this the first request fails with a connection reset that
# looks like a network problem.
if ([System.Net.ServicePointManager]::SecurityProtocol -band [System.Net.SecurityProtocolType]::Tls12) {
    # Already enabled.
}
else {
    [System.Net.ServicePointManager]::SecurityProtocol =
    [System.Net.ServicePointManager]::SecurityProtocol -bor [System.Net.SecurityProtocolType]::Tls12
}

if (-not $Version) { $Version = Get-LatestVersion }
if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'signpost\bin' }

$arch = Get-Arch
$name = "signpost_${Version}_windows_${arch}"
$archive = "$name.zip"
$base = "https://github.com/$Repo/releases/download/$Version"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("signpost-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $archivePath = Join-Path $tmp $archive
    Write-Step "downloading $archive"
    try {
        Invoke-WebRequest -Uri "$base/$archive" -OutFile $archivePath -UseBasicParsing
    }
    catch {
        throw "no release asset $archive for $Version — check the version and your platform"
    }

    $sumsPath = Join-Path $tmp 'checksums.txt'
    Write-Step 'downloading checksums.txt'
    try {
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sumsPath -UseBasicParsing
    }
    catch {
        throw "release $Version publishes no checksums.txt; refusing to install unverified"
    }

    $expected = $null
    foreach ($line in Get-Content $sumsPath) {
        $fields = $line -split '\s+', 2
        if ($fields.Count -lt 2) { continue }
        # sha256sum writes a leading '*' for a file it read in binary mode.
        if ($fields[1].Trim().TrimStart('*') -eq $archive) {
            $expected = $fields[0].Trim()
            break
        }
    }
    if (-not $expected) {
        throw "$archive is not listed in checksums.txt; refusing to install"
    }

    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
    if ($actual -ne $expected.ToUpperInvariant()) {
        throw @"
checksum mismatch for $archive
  expected $($expected.ToUpperInvariant())
  got      $actual
Not installing. Either the download was corrupted or the asset is not the one that
was published.
"@
    }
    Write-Step 'sha256 verified'

    Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
    $staged = Join-Path (Join-Path $tmp $name) $Bin
    if (-not (Test-Path $staged)) {
        throw "archive does not contain $name\$Bin"
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $dest = Join-Path $InstallDir $Bin
    # A running signpost.exe cannot be overwritten on Windows, but it can be renamed
    # out of the way. Without this an upgrade fails with a file-in-use error while
    # any shell still has the old binary open.
    if (Test-Path $dest) {
        $old = "$dest.old"
        if (Test-Path $old) { Remove-Item $old -Force -ErrorAction SilentlyContinue }
        try { Move-Item $dest $old -Force } catch { }
    }
    Move-Item $staged $dest -Force
    Write-Step "installed signpost $Version to $dest"

    if (-not $NoPath) {
        # User PATH only. Writing the machine PATH needs elevation, and a script run
        # from the internet should not be asking for it.
        $userPath = [System.Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @()
        if ($userPath) { $entries = $userPath -split ';' | Where-Object { $_ } }
        if ($entries -notcontains $InstallDir) {
            $updated = (@($entries) + $InstallDir) -join ';'
            [System.Environment]::SetEnvironmentVariable('Path', $updated, 'User')
            Write-Step "added $InstallDir to your user PATH"
            Write-Host 'Open a new terminal for it to take effect.'
        }
        # The current session too, so `signpost version` works without reopening.
        if (($env:Path -split ';') -notcontains $InstallDir) {
            $env:Path = "$env:Path;$InstallDir"
        }
    }
}
finally {
    # Removed on every path, including a failed verification: a rejected archive must
    # not be left in the temp directory where someone might unpack it by hand.
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
