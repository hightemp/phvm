# phvm installer script for Windows
# Usage: iwr -useb https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

$PhvmVersion = if ($env:PHVM_VERSION) { $env:PHVM_VERSION } else { "latest" }
$PhvmDir = if ($env:PHVM_DIR) { $env:PHVM_DIR } else { "$env:USERPROFILE\.phvm" }
$PhvmBinDir = Join-Path $PhvmDir "bin"
$GithubRepo = "hightemp/phvm"

function Write-Info($msg) {
    Write-Host "[*] " -ForegroundColor Green -NoNewline
    Write-Host $msg
}

function Write-Warn($msg) {
    Write-Host "[!] " -ForegroundColor Yellow -NoNewline
    Write-Host $msg
}

function Write-Err($msg) {
    Write-Host "[x] " -ForegroundColor Red -NoNewline
    Write-Host $msg
    throw $msg
}

function Write-Success($msg) {
    Write-Host "[+] " -ForegroundColor Green -NoNewline
    Write-Host $msg
}

function Get-Platform {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "windows_amd64" }
        "Arm64" { return "windows_arm64" }
        default { Write-Err "Unsupported architecture: $arch" }
    }
}

function Get-LatestVersion {
    $metadataFile = [IO.Path]::GetTempFileName()
    try {
        Save-PhvmDownload "https://api.github.com/repos/$GithubRepo/releases/latest" $metadataFile
        $release = [IO.File]::ReadAllText($metadataFile) | ConvertFrom-Json
        return $release.tag_name
    } catch {
        Write-Err 'Failed to get latest version'
    } finally {
        Remove-Item -LiteralPath $metadataFile -Force -ErrorAction SilentlyContinue
    }
}

function Save-PhvmDownload([string]$Uri, [string]$Destination) {
    $current = [Uri]$Uri
    for ($redirect = 0; $redirect -lt 10; $redirect++) {
        if ($current.Scheme -ne 'https' -or $current.UserInfo) { throw 'Only HTTPS downloads are allowed' }
        try {
            $response = Invoke-WebRequest -Uri $current -OutFile $Destination -UseBasicParsing -MaximumRedirection 0 -PassThru
        } catch {
            $response = $_.Exception.Response
            if (-not $response) { throw 'Release download failed' }
        }
        $status = [int]$response.StatusCode
        if ($status -eq 200) { return }
        if ($status -notin @(301, 302, 303, 307, 308) -or -not $response.Headers.Location) { throw 'Release download failed' }
        $current = [Uri]::new($current, $response.Headers.Location.ToString())
    }
    throw 'Too many release redirects'
}

function Test-PhvmArchive([string]$Archive, [string]$Checksums, [string]$Asset) {
    $assetHashes = @()
    foreach ($line in [IO.File]::ReadAllLines($Checksums)) {
        $parts = $line.Trim().Split([char[]]" `t", [StringSplitOptions]::RemoveEmptyEntries)
        $filename = if ($parts.Count -ge 2) { $parts[1] } else { '' }
        if ($filename.StartsWith('*')) { $filename = $filename.Substring(1) }
        if ($parts.Count -ge 2 -and $filename -ceq $Asset) {
            if ($parts.Count -ne 2 -or $parts[0] -notmatch '^[a-fA-F0-9]{64}$') { throw "Invalid SHA256 entry for $Asset" }
            $assetHashes += $parts[0].ToLowerInvariant()
        }
    }
    if ($assetHashes.Count -ne 1) { throw "Expected exactly one checksum for $Asset" }
    $actual = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -cne $assetHashes[0]) { throw "SHA256 mismatch for $Asset" }
}

function Publish-PhvmArchive([string]$Archive, [string]$Stage, [string]$Destination) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($Archive)
    $candidate = Join-Path $Stage 'phvm.exe'
    try {
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq 'phvm.exe' })
        if ($entries.Count -ne 1) { throw 'Expected exactly one phvm.exe binary' }
        $entry = $entries[0]
        $unixType = ($entry.ExternalAttributes -shr 16) -band 0xf000
        if ($unixType -notin @(0, 0x8000) -or $entry.Length -le 0 -or $entry.Length -gt 256MB) { throw 'Archive binary must be a regular file within size limit' }
        $inputStream = $entry.Open()
        try {
            $outputStream = [IO.File]::Open($candidate, [IO.FileMode]::CreateNew)
            try { $inputStream.CopyTo($outputStream); $outputStream.Flush($true) } finally { $outputStream.Dispose() }
        } finally { $inputStream.Dispose() }
    } finally { $zip.Dispose() }
    if (Test-Path -LiteralPath $Destination) {
        $existing = Get-Item -LiteralPath $Destination -Force
        if ($existing.PSIsContainer -or ($existing.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Refusing a non-regular destination binary' }
        [IO.File]::Replace($candidate, $Destination, (Join-Path $Stage 'previous.exe'))
    } else {
        [IO.File]::Move($candidate, $Destination)
    }
}

function Install-Phvm {
    Write-Host ""
    Write-Host "  ██████╗ ██╗  ██╗██╗   ██╗███╗   ███╗" -ForegroundColor Cyan
    Write-Host "  ██╔══██╗██║  ██║██║   ██║████╗ ████║" -ForegroundColor Cyan
    Write-Host "  ██████╔╝███████║██║   ██║██╔████╔██║" -ForegroundColor Cyan
    Write-Host "  ██╔═══╝ ██╔══██║╚██╗ ██╔╝██║╚██╔╝██║" -ForegroundColor Cyan
    Write-Host "  ██║     ██║  ██║ ╚████╔╝ ██║ ╚═╝ ██║" -ForegroundColor Cyan
    Write-Host "  ╚═╝     ╚═╝  ╚═╝  ╚═══╝  ╚═╝     ╚═╝" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  PHP Version Manager" -ForegroundColor White
    Write-Host ""

    # Detect platform
    $platform = Get-Platform
    Write-Info "Detected platform: $platform"

    # Get version
    if ($PhvmVersion -eq "latest") {
        Write-Info "Fetching latest version..."
        $PhvmVersion = Get-LatestVersion
    }
    Write-Info "Installing phvm $PhvmVersion"
    if ($PhvmVersion -notmatch '^v?\d+\.\d+\.\d+([-+][a-zA-Z0-9.-]+)?$') { throw 'Invalid release version' }

    # Create directories
    New-Item -ItemType Directory -Force -Path $PhvmBinDir | Out-Null

    # Download
    $version = $PhvmVersion.TrimStart('v')
    $asset = "phvm_${version}_${platform}.zip"
    $releaseUrl = "https://github.com/$GithubRepo/releases/download/$PhvmVersion"
    $downloadUrl = "$releaseUrl/$asset"
    $stage = Join-Path $PhvmBinDir ('.install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $stage | Out-Null
    $tempFile = Join-Path $stage 'archive.zip'
    $checksums = Join-Path $stage 'checksums.txt'
    $phvmExe = Join-Path $PhvmBinDir 'phvm.exe'

    Write-Info "Downloading from $downloadUrl"
    try {
        Save-PhvmDownload $downloadUrl $tempFile
        Save-PhvmDownload "$releaseUrl/checksums.txt" $checksums
        Test-PhvmArchive $tempFile $checksums $asset
        Write-Success 'SHA256 verified'
        Publish-PhvmArchive $tempFile $stage $phvmExe
    } finally {
        Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
    }

    # Verify
    if (-not (Test-Path $phvmExe)) {
        Write-Err "Installation failed: binary not found"
    }

    Write-Success "phvm installed to $phvmExe"

    # Create directory structure
    Write-Info "Creating directory structure..."
    $dirs = @(
        "$PhvmDir\versions\php",
        "$PhvmDir\alias",
        "$PhvmDir\cache\downloads",
        "$PhvmDir\config",
        "$PhvmDir\logs"
    )
    foreach ($dir in $dirs) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }

    # Add to PATH
    Write-Host ""
    Write-Success "Installation complete!"
    Write-Host ""

    $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($currentPath -notlike "*$PhvmBinDir*") {
        Write-Host "To add phvm to your PATH, run:" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "    `$env:PATH = `"$PhvmBinDir;`$env:PATH`"" -ForegroundColor White
        Write-Host ""
        Write-Host "To make it permanent, run:" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "    [Environment]::SetEnvironmentVariable('PATH', `"$PhvmBinDir;`$env:PATH`", 'User')" -ForegroundColor White
        Write-Host ""
    }

    Write-Host "To enable shell integration, add to your PowerShell profile:" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "    Invoke-Expression (& $phvmExe init powershell)" -ForegroundColor White
    Write-Host ""
    Write-Host "Get started:" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "    phvm doctor          # Check build dependencies" -ForegroundColor White
    Write-Host "    phvm ls-remote       # List available versions" -ForegroundColor White
    Write-Host "    phvm install 8.3     # Install PHP 8.3" -ForegroundColor White
    Write-Host ""
}

if ($MyInvocation.InvocationName -ne '.') { Install-Phvm }
