# ETC Scraper Uninstaller for Windows (PowerShell)
# Usage: irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/uninstall.ps1 | iex

$ErrorActionPreference = "SilentlyContinue"

$InstallDir = "$env:LOCALAPPDATA\etc-scraper"
$BinaryName = "etc-scraper.exe"
$ServiceName = "etc-scraper"

Write-Host "=== ETC Scraper Uninstaller ===" -ForegroundColor Yellow

# Check if running as admin
$IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

# Check if service exists
$Service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue

if ($Service) {
    if (-not $IsAdmin) {
        Write-Host "Error: Service '$ServiceName' is installed." -ForegroundColor Red
        Write-Host "Please run PowerShell as Administrator to uninstall." -ForegroundColor Red
        exit 1
    }

    # Stop service first
    Write-Host "Stopping service '$ServiceName'..."
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    # Remove service
    Write-Host "Removing service '$ServiceName'..."
    $ExePath = Join-Path $InstallDir $BinaryName
    if (Test-Path $ExePath) {
        & $ExePath -service uninstall 2>&1 | Out-Null
    }
    # Fallback: use sc.exe
    sc.exe delete $ServiceName 2>&1 | Out-Null
    Start-Sleep -Seconds 1
    Write-Host "Service removed." -ForegroundColor Green
}

# Remove from PATH
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -like "*$InstallDir*") {
    Write-Host "Removing from PATH..."
    $NewPath = ($UserPath.Split(';') | Where-Object { $_ -ne $InstallDir -and $_ -ne "" }) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    Write-Host "PATH updated."
}

# Remove installation directory
if (Test-Path $InstallDir) {
    Write-Host "Removing $InstallDir..."
    Remove-Item -Path $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
    if (Test-Path $InstallDir) {
        Write-Host "Warning: Could not remove all files. Some files may be in use." -ForegroundColor Yellow
        Write-Host "Try closing any terminals using etc-scraper and run again."
    } else {
        Write-Host "Installation directory removed." -ForegroundColor Green
    }
} else {
    Write-Host "Installation directory not found: $InstallDir" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Uninstallation complete!" -ForegroundColor Green
Write-Host "Note: Restart your terminal for PATH changes to take effect."
