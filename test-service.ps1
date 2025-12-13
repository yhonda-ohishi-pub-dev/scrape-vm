$ErrorActionPreference = "SilentlyContinue"
$serviceName = "etc-scraper"
$installDir = "$env:LOCALAPPDATA\etc-scraper"
$sourceExe = "C:\googlecloud\scrape-vm\etc-scraper.exe"
$logFile = "$installDir\test-output.log"

function Log($msg, $c) {
    if (!$c) { $c = "White" }
    $t = Get-Date -Format "HH:mm:ss"
    Write-Host "[$t] $msg" -ForegroundColor $c
    "[$t] $msg" | Add-Content $logFile
}

function CheckStatus($name) {
    Log "--- $name ---" Cyan
    $svc = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    Log "Status: $($svc.Status)" Yellow
    $events = Get-EventLog -LogName Application -Source $serviceName -Newest 3 -ErrorAction SilentlyContinue
    foreach ($e in $events) { Log "  $($e.Message)" White }
    $svcLog = "$installDir\logs\etc-scraper.log"
    if (Test-Path $svcLog) { Get-Content $svcLog -Tail 10 | ForEach-Object { Log "  $_" Gray } }
    return $svc.Status
}

"=== Test $(Get-Date) ===" | Out-File $logFile
Log "=== Setup ===" Cyan
Log "[1] Stop" Yellow; Stop-Service $serviceName -Force; Stop-Process -Name "etc-scraper" -Force -ErrorAction SilentlyContinue
Log "[2] Uninstall" Yellow; & "$installDir\etc-scraper.exe" -service uninstall 2>$null
Log "[3] Copy" Yellow; Copy-Item $sourceExe "$installDir\etc-scraper.exe" -Force
Log "[4] Clear log" Yellow; Remove-Item "$installDir\logs\etc-scraper.log" -Force -ErrorAction SilentlyContinue
Log "[5] Install" Yellow; Push-Location $installDir; .\etc-scraper.exe -service install; Pop-Location
Log "[6] Start" Yellow; & "$installDir\etc-scraper.exe" -service start
Log "Wait 3s..." Cyan; Start-Sleep 3
$s1 = CheckStatus "Check1"
Log "Wait 5s..." Magenta; Start-Sleep 5
$s2 = CheckStatus "Check2"
Log "=== SUMMARY ===" Green
Log "Initial: $s1, After 5s: $s2" White
if ($s1 -eq "Running" -and $s2 -eq "Running") { Log "SUCCESS!" Green }
elseif ($s1 -eq "Running") { Log "CRASHED!" Red }
else { Log "FAILED TO START" Red }
