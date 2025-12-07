# ETC Scraper Makefile (Git Bash on Windows)

# VM設定
VM_NAME := instance-20251207-115015
VM_ZONE := asia-northeast1-b
REMOTE_PATH := /opt/etc-scraper/etc-scraper

# ビルド設定
BINARY_LINUX := etc-scraper-linux
BINARY_WIN := etc-scraper.exe

.PHONY: all build build-linux build-windows upload deploy ssh clean help

# デフォルト
all: deploy

# Linux用ビルド
build-linux:
	@echo "=== Building for Linux ==="
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY_LINUX) .
	@ls -la $(BINARY_LINUX)

# Windows用ビルド
build-windows:
	@echo "=== Building for Windows ==="
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY_WIN) .
	@ls -la $(BINARY_WIN)

# 両方ビルド
build: build-linux build-windows

# VMにアップロード
upload: build-linux
	@echo "=== Uploading to VM ==="
	gcloud compute scp $(BINARY_LINUX) $(VM_NAME):/tmp/$(BINARY_LINUX) --zone=$(VM_ZONE)
	@echo "Upload complete!"

# VMにデプロイ（アップロード＋配置）
deploy: upload
	@echo "=== Deploying on VM ==="
	gcloud compute ssh $(VM_NAME) --zone=$(VM_ZONE) -- "sudo mv /tmp/$(BINARY_LINUX) $(REMOTE_PATH) && sudo chmod +x $(REMOTE_PATH) && $(REMOTE_PATH) --help"
	@echo "=== Deploy Complete ==="

# VMにSSH接続
ssh:
	gcloud compute ssh $(VM_NAME) --zone=$(VM_ZONE)

# クリーンアップ
clean:
	rm -f $(BINARY_LINUX) $(BINARY_WIN)

# ヘルプ
help:
	@echo "Usage:"
	@echo "  make deploy  - Build and deploy to VM"
	@echo "  make upload  - Build and upload only"
	@echo "  make build   - Build both binaries"
	@echo "  make ssh     - SSH to VM"
	@echo "  make clean   - Remove binaries"
