# ETC Scraper Uninstaller for Windows (PowerShell)
# Usage: irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/uninstall.ps1 | iex

$ErrorActionPreference = "Stop"

$InstallDir = "$env:LOCALAPPDATA\etc-scraper"
$BinaryName = "etc-scraper.exe"
$ServiceName = "etc-scraper"

Write-Host "=== ETC Scraper Uninstaller ===" -ForegroundColor Yellow

# Check if running as admin (needed for service removal)
$IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

# Stop and remove service if exists
$Service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($Service) {
    if (-not $IsAdmin) {
        Write-Host "Warning: Service '$ServiceName' is installed. Run as Administrator to remove it." -ForegroundColor Yellow
    } else {
        Write-Host "Stopping service '$ServiceName'..."
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue

        Write-Host "Removing service '$ServiceName'..."
        $ExePath = Join-Path $InstallDir $BinaryName
        if (Test-Path $ExePath) {
            & $ExePath -service uninstall 2>&1 | Out-Null
        }
        # Fallback: use sc.exe
        sc.exe delete $ServiceName 2>&1 | Out-Null
        Write-Host "Service removed." -ForegroundColor Green
    }
}

# Remove from PATH
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -like "*$InstallDir*") {
    Write-Host "Removing from PATH..."
    $NewPath = ($UserPath.Split(';') | Where-Object { $_ -ne $InstallDir }) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    Write-Host "PATH updated."
}

# Remove installation directory
if (Test-Path $InstallDir) {
    Write-Host "Removing $InstallDir..."
    Remove-Item -Path $InstallDir -Recurse -Force
    Write-Host "Installation directory removed." -ForegroundColor Green
} else {
    Write-Host "Installation directory not found: $InstallDir" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Uninstallation complete!" -ForegroundColor Green
Write-Host "Note: Restart your terminal for PATH changes to take effect."
