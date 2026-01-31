# phvm installer script for Windows
# Usage: iwr -useb https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

$PhvmVersion = if ($env:PHVM_VERSION) { $env:PHVM_VERSION } else { "latest" }
$PhvmDir = if ($env:PHVM_DIR) { $env:PHVM_DIR } else { "$env:USERPROFILE\.phvm" }
$PhvmBinDir = "$PhvmDir\bin"
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
    exit 1
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
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$GithubRepo/releases/latest"
        return $release.tag_name
    } catch {
        Write-Err "Failed to get latest version: $_"
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

    # Create directories
    New-Item -ItemType Directory -Force -Path $PhvmBinDir | Out-Null

    # Download
    $version = $PhvmVersion.TrimStart('v')
    $downloadUrl = "https://github.com/$GithubRepo/releases/download/$PhvmVersion/phvm_${version}_${platform}.zip"
    $tempFile = [System.IO.Path]::GetTempFileName() + ".zip"

    Write-Info "Downloading from $downloadUrl"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempFile -UseBasicParsing
    } catch {
        Write-Err "Failed to download phvm: $_"
    }

    # Extract
    Write-Info "Extracting..."
    try {
        Expand-Archive -Path $tempFile -DestinationPath $PhvmBinDir -Force
    } catch {
        Write-Err "Failed to extract: $_"
    }
    Remove-Item -Path $tempFile -Force

    # Verify
    $phvmExe = "$PhvmBinDir\phvm.exe"
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

Install-Phvm
