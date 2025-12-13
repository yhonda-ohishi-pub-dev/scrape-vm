# ETC Scraper Installer for Windows (PowerShell)
# Usage: irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.ps1 | iex
# With service: $env:INSTALL_SERVICE = "true"; irm ... | iex

$ErrorActionPreference = "Stop"

$Repo = "yhonda-ohishi-pub-dev/scrape-vm"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\etc-scraper" }
$BinaryName = "etc-scraper.exe"
$InstallService = $env:INSTALL_SERVICE -eq "true"

Write-Host "=== ETC Scraper Installer ===" -ForegroundColor Green

# Check admin for service installation
$IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($InstallService -and -not $IsAdmin) {
    Write-Host "Error: Service installation requires Administrator privileges." -ForegroundColor Red
    Write-Host "Please run PowerShell as Administrator and try again."
    exit 1
}

# Get latest release tag
Write-Host "Fetching latest release..."
$Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
$LatestTag = $Release.tag_name

if (-not $LatestTag) {
    Write-Host "Error: Could not fetch latest release" -ForegroundColor Red
    exit 1
}

Write-Host "Latest version: $LatestTag"

# Download URL
$DownloadUrl = "https://github.com/$Repo/releases/download/$LatestTag/etc-scraper_${LatestTag}_windows_amd64.zip"

# Stop existing service and processes
$ExistingExe = Join-Path $InstallDir $BinaryName
if (Test-Path $ExistingExe) {
    Write-Host "Stopping existing service..." -ForegroundColor Yellow
    $ErrorActionPreference = "SilentlyContinue"
    & $ExistingExe -service stop 2>&1 | Out-Null
    Start-Sleep -Seconds 2
    & $ExistingExe -service uninstall 2>&1 | Out-Null
    Start-Sleep -Seconds 1
    $ErrorActionPreference = "Stop"
}
Stop-Process -Name "etc-scraper" -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# Create install directory
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Create temp directory
$TmpDir = Join-Path $env:TEMP "etc-scraper-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    # Download
    Write-Host "Downloading $DownloadUrl..."
    $ZipPath = Join-Path $TmpDir "etc-scraper.zip"
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath

    # Extract
    Write-Host "Extracting..."
    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    # Install
    Write-Host "Installing to $InstallDir..."
    Copy-Item -Path (Join-Path $TmpDir $BinaryName) -Destination $InstallDir -Force

    # Verify
    $ExePath = Join-Path $InstallDir $BinaryName
    $Version = & $ExePath -version 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Successfully installed!" -ForegroundColor Green
        Write-Host $Version
    } else {
        Write-Host "Installation failed" -ForegroundColor Red
        exit 1
    }

    # Check PATH
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        Write-Host ""
        Write-Host "Adding $InstallDir to PATH..." -ForegroundColor Yellow
        [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "PATH updated. Restart your terminal to use 'etc-scraper' command."
    }

    # Install as service if requested
    if ($InstallService) {
        # P2P setup first (OAuth authentication)
        Write-Host ""
        Write-Host "Setting up P2P credentials (OAuth authentication)..." -ForegroundColor Cyan
        Write-Host "A browser window will open for authentication." -ForegroundColor Yellow

        # Run p2p-setup with credentials saved to install directory
        $CredsPath = Join-Path $InstallDir "p2p_credentials.env"
        & $ExePath -p2p-setup -p2p-creds $CredsPath

        if ($LASTEXITCODE -ne 0) {
            Write-Host "P2P setup failed or was cancelled." -ForegroundColor Red
            Write-Host "You can run setup later with: etc-scraper.exe -p2p-setup" -ForegroundColor Yellow
        } elseif (-not (Test-Path $CredsPath)) {
            Write-Host "Credentials file not created. Service may not start properly." -ForegroundColor Yellow
        } else {
            Write-Host "P2P credentials saved!" -ForegroundColor Green
        }

        # Install service
        Write-Host ""
        Write-Host "Installing Windows Service..." -ForegroundColor Cyan
        & $ExePath -service install
        if ($LASTEXITCODE -eq 0) {
            Write-Host "Starting service..." -ForegroundColor Cyan
            & $ExePath -service start
            if ($LASTEXITCODE -eq 0) {
                Write-Host "Service installed and started!" -ForegroundColor Green
            } else {
                Write-Host "Service installed but failed to start. Check logs." -ForegroundColor Yellow
            }
        } else {
            Write-Host "Failed to install service." -ForegroundColor Red
        }
    }

    Write-Host ""
    Write-Host "Done! Run 'etc-scraper.exe -help' to get started." -ForegroundColor Green

    if (-not $InstallService) {
        Write-Host ""
        Write-Host "To install as Windows Service (run as Administrator):" -ForegroundColor Cyan
        Write-Host '  $env:INSTALL_SERVICE = "true"; irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.ps1 | iex'
    }

} finally {
    # Cleanup
    Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
