# ETC Scraper Makefile (Windows - PowerShell)

# VM設定
VM_NAME := instance-20251207-115015
VM_ZONE := asia-northeast1-b

# ビルド設定
BINARY_LINUX := etc-scraper-linux
BINARY_WIN := etc-scraper.exe

# PowerShell command
PS := powershell -ExecutionPolicy Bypass

.PHONY: all build build-linux build-windows deploy ssh tunnel health clean help

# デフォルト
all: deploy

# Linux用ビルド
build-linux:
	@echo "=== Building for Linux ==="
	$(PS) -Command "$$env:GOOS='linux'; $$env:GOARCH='amd64'; $$env:CGO_ENABLED='0'; go build -ldflags '-s -w' -o $(BINARY_LINUX) ."

# Windows用ビルド
build-windows:
	@echo "=== Building for Windows ==="
	$(PS) -Command "$$env:GOOS='windows'; $$env:GOARCH='amd64'; $$env:CGO_ENABLED='0'; go build -ldflags '-s -w' -o $(BINARY_WIN) ."

# 両方ビルド
build: build-linux build-windows

# VMにデプロイ（ビルド＋アップロード＋配置＋サービス登録）
deploy:
	$(PS) -File deploy.ps1

# VMにSSH接続
ssh:
	$(PS) -File gcloud.ps1 compute ssh $(VM_NAME) --zone=$(VM_ZONE)

# IAP トンネル (gRPC用)
tunnel:
	@echo "=== Starting IAP Tunnel to port 50051 ==="
	@echo "Press Ctrl+C to stop"
	$(PS) -File gcloud.ps1 compute start-iap-tunnel $(VM_NAME) 50051 --local-host-port=localhost:50051 --zone=$(VM_ZONE)

# ヘルスチェック
health:
	$(PS) -File gcloud.ps1 compute ssh $(VM_NAME) --zone=$(VM_ZONE) --command="grpcurl -plaintext localhost:50051 scraper.ETCScraper/Health"

# クリーンアップ
clean:
	rm -f $(BINARY_LINUX) $(BINARY_WIN)

# ヘルプ
help:
	@echo "Usage:"
	@echo "  make deploy  - Build and deploy to VM (uses deploy.ps1)"
	@echo "  make build   - Build both binaries"
	@echo "  make ssh     - SSH to VM"
	@echo "  make tunnel  - Start IAP tunnel to gRPC port"
	@echo "  make health  - Check service health on VM"
	@echo "  make clean   - Remove binaries"
