# ETC Scraper Makefile (Git Bash on Windows)

# VM設定
VM_NAME := instance-20251207-115015
VM_ZONE := asia-northeast1-b
REMOTE_PATH := /opt/etc-scraper/etc-scraper
SERVICE_FILE := etc-scraper.service
SERVICE_NAME := etc-scraper

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
	gcloud compute scp $(SERVICE_FILE) $(VM_NAME):/tmp/$(SERVICE_FILE) --zone=$(VM_ZONE)
	@echo "Upload complete!"

# VMにデプロイ（アップロード＋配置＋サービス登録）
deploy: upload
	@echo "=== Deploying on VM ==="
	gcloud compute ssh $(VM_NAME) --zone=$(VM_ZONE) -- "\
		sudo mkdir -p /opt/etc-scraper/downloads && \
		sudo mv /tmp/$(BINARY_LINUX) $(REMOTE_PATH) && \
		sudo chmod +x $(REMOTE_PATH) && \
		sudo mv /tmp/$(SERVICE_FILE) /etc/systemd/system/$(SERVICE_FILE) && \
		sudo systemctl daemon-reload && \
		sudo systemctl enable $(SERVICE_NAME) && \
		sudo systemctl restart $(SERVICE_NAME) && \
		sleep 2 && \
		sudo systemctl status $(SERVICE_NAME)"
	@echo "=== Deploy Complete ==="
	@echo "Service '$(SERVICE_NAME)' is now running on port 50051"

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
