# ETC Scraper Deploy Script for Windows

$VM_NAME = "instance-20251207-115015"
$VM_ZONE = "asia-northeast1-b"
$BINARY_LINUX = "etc-scraper-linux"
$REMOTE_PATH = "/opt/etc-scraper/etc-scraper"

# gcloud path
$GCLOUD = "$env:LOCALAPPDATA\Google\Cloud SDK\google-cloud-sdk\bin\gcloud.cmd"

Write-Host "=== Building for Linux ===" -ForegroundColor Cyan
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -ldflags "-s -w" -o $BINARY_LINUX .

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "Built: $BINARY_LINUX" -ForegroundColor Green

Write-Host "=== Uploading to VM ===" -ForegroundColor Cyan
& $GCLOUD compute scp $BINARY_LINUX "${VM_NAME}:/tmp/${BINARY_LINUX}" --zone=$VM_ZONE

if ($LASTEXITCODE -ne 0) {
    Write-Host "Upload failed!" -ForegroundColor Red
    exit 1
}
Write-Host "Upload complete!" -ForegroundColor Green

Write-Host "=== Deploying on VM ===" -ForegroundColor Cyan
& $GCLOUD compute ssh $VM_NAME --zone=$VM_ZONE --command="sudo mv /tmp/$BINARY_LINUX $REMOTE_PATH && sudo chmod +x $REMOTE_PATH && $REMOTE_PATH --help"

if ($LASTEXITCODE -ne 0) {
    Write-Host "Deploy failed!" -ForegroundColor Red
    exit 1
}

Write-Host "=== Deploy Complete! ===" -ForegroundColor Green
