# ETC Scraper Makefile (Windows - PowerShell)

# VM設定
VM_NAME := instance-20251207-115015
VM_ZONE := asia-northeast1-b

# ビルド設定
BINARY_LINUX := etc-scraper-linux
BINARY_WIN := etc-scraper.exe

# バージョン情報（タグから取得、なければdev）
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y%m%dT%H%M%SZ 2>/dev/null || echo "unknown")

# ldflags for version embedding
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

# PowerShell command
PS := powershell -ExecutionPolicy Bypass

.PHONY: all build build-linux build-windows deploy ssh tunnel health clean help version

# デフォルト
all: deploy

# Linux用ビルド
build-linux:
	@echo "=== Building for Linux ($(VERSION)) ==="
	$(PS) -Command "$$env:GOOS='linux'; $$env:GOARCH='amd64'; $$env:CGO_ENABLED='0'; go build -ldflags '$(LDFLAGS)' -o $(BINARY_LINUX) ."

# Windows用ビルド
build-windows:
	@echo "=== Building for Windows ($(VERSION)) ==="
	$(PS) -Command "$$env:GOOS='windows'; $$env:GOARCH='amd64'; $$env:CGO_ENABLED='0'; go build -ldflags '$(LDFLAGS)' -o $(BINARY_WIN) ."

# 両方ビルド
build: build-linux build-windows

# バージョン表示
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(GIT_COMMIT)"
	@echo "Build:   $(BUILD_TIME)"

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
	@echo "  make build   - Build both binaries with version info"
	@echo "  make version - Show version info"
	@echo "  make ssh     - SSH to VM"
	@echo "  make tunnel  - Start IAP tunnel to gRPC port"
	@echo "  make health  - Check service health on VM"
	@echo "  make clean   - Remove binaries"
	@echo ""
	@echo "Windows Service (run as Administrator):"
	@echo "  etc-scraper.exe -service install   - Install Windows service"
	@echo "  etc-scraper.exe -service start     - Start service"
	@echo "  etc-scraper.exe -service stop      - Stop service"
	@echo "  etc-scraper.exe -service uninstall - Uninstall service"
	@echo ""
	@echo "Updates:"
	@echo "  etc-scraper.exe -check-update      - Check for updates"
	@echo "  etc-scraper.exe -version           - Show version"
