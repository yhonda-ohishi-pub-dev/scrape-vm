#!/bin/bash
# ETC Scraper Deploy Script

VM_NAME="instance-20251207-115015"
VM_ZONE="asia-northeast1-b"
BINARY_LINUX="etc-scraper-linux"
REMOTE_PATH="/opt/etc-scraper/etc-scraper"

# gcloudのパス - 8.3短縮名を使用（スペース問題回避）
# "Cloud SDK" -> "CLOUDS~1"
GCLOUD='/c/Users/mtama/AppData/Local/Google/CLOUDS~1/google-cloud-sdk/bin/gcloud.cmd'

# スクリプトのディレクトリに移動
cd "$(dirname "$0")"

echo "=== Building for Linux ==="
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o $BINARY_LINUX .
ls -la $BINARY_LINUX

echo "=== Uploading to VM ==="
"$GCLOUD" compute scp $BINARY_LINUX $VM_NAME:/tmp/$BINARY_LINUX --zone=$VM_ZONE

echo "=== Installing on VM ==="
"$GCLOUD" compute ssh $VM_NAME --zone=$VM_ZONE -- "sudo mv /tmp/$BINARY_LINUX $REMOTE_PATH && sudo chmod +x $REMOTE_PATH && $REMOTE_PATH --help"

echo "=== Deploy Complete ==="
