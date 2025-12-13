# ETC Scraper Makefile

# VM設定
VM_NAME := instance-20251207-115015
VM_ZONE := asia-northeast1-b

# ビルド設定
BINARY_LINUX := etc-scraper-linux
BINARY_WIN := etc-scraper.exe
UPDATER_WIN := etc-scraper-updater.exe

# バージョン情報（タグから取得、なければdev）
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y%m%dT%H%M%SZ 2>/dev/null || echo "unknown")

# ldflags for version embedding
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

# PowerShell command
PS := powershell -ExecutionPolicy Bypass

.PHONY: all build build-linux build-windows build-updater deploy ssh tunnel health clean help version release release-zip

# デフォルト
all: deploy

# Linux用ビルド
build-linux:
	@echo "=== Building for Linux ($(VERSION)) ==="
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o $(BINARY_LINUX) .

# Windows用ビルド
build-windows:
	@echo "=== Building for Windows ($(VERSION)) ==="
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o $(BINARY_WIN) .

# Windows用Updaterビルド
build-updater:
	@echo "=== Building Updater for Windows ($(VERSION)) ==="
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o $(UPDATER_WIN) ./cmd/updater/

# 両方ビルド
build: build-linux build-windows build-updater

# バージョン表示
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(GIT_COMMIT)"
	@echo "Build:   $(BUILD_TIME)"

# リリース用zip作成（メインバイナリ + Updater）
release-zip: build-windows build-updater
	@echo "=== Creating release zip ==="
	$(PS) -Command "Compress-Archive -Path '$(BINARY_WIN)','$(UPDATER_WIN)' -DestinationPath 'etc-scraper_$(VERSION)_windows_amd64.zip' -Force"
	@echo "Created: etc-scraper_$(VERSION)_windows_amd64.zip"

# GitHub Release作成（タグ必須）
release: release-zip
	@echo "=== Creating GitHub Release $(VERSION) ==="
	gh release create $(VERSION) etc-scraper_$(VERSION)_windows_amd64.zip --title "$(VERSION)" --generate-notes

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
	rm -f $(BINARY_LINUX) $(BINARY_WIN) $(UPDATER_WIN) *.zip

# ヘルプ
help:
	@echo "Usage:"
	@echo "  make deploy      - Build and deploy to VM (uses deploy.ps1)"
	@echo "  make build       - Build all binaries with version info"
	@echo "  make build-windows - Build Windows binary only"
	@echo "  make build-linux - Build Linux binary only"
	@echo "  make build-updater - Build Windows updater binary"
	@echo "  make version     - Show version info"
	@echo "  make release-zip - Build and create release zip"
	@echo "  make release     - Create GitHub release (requires tag)"
	@echo "  make ssh         - SSH to VM"
	@echo "  make tunnel      - Start IAP tunnel to gRPC port"
	@echo "  make health      - Check service health on VM"
	@echo "  make clean       - Remove binaries and zips"
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
	@echo ""
	@echo "Auto-Updater Service (run as Administrator):"
	@echo "  etc-scraper-updater.exe -service install   - Install updater service"
	@echo "  etc-scraper-updater.exe -service start     - Start updater service"
	@echo "  etc-scraper-updater.exe -service stop      - Stop updater service"
	@echo "  etc-scraper-updater.exe -service uninstall - Uninstall updater service"
