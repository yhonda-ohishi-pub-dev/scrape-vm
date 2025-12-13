# ETC Scraper

ETC利用照会サービス（etc-meisai.jp）から利用明細CSVを自動ダウンロードするスクレイパーツール。

## 機能

- chromedpを使用したheadlessブラウザ自動化
- gRPCサーバーモードでリモートからスクレイピング実行可能
- P2Pモード（WebRTC経由でブラウザから直接制御可能）
- 複数アカウント対応
- 新しいタブで開くCSVダウンロードに対応

## 要件

- Google Chrome（headlessモード用）
- Windows

## インストール

### ワンライナーインストール（推奨）

PowerShellで以下を実行:

```powershell
irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.ps1 | iex
```

デフォルトで `%LOCALAPPDATA%\etc-scraper` にインストールされ、PATHに自動追加されます。

インストール先を変更する場合:

```powershell
$env:INSTALL_DIR = "C:\path\to\dir"; irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.ps1 | iex
```

### Windowsサービスとして登録

管理者権限のPowerShellで、サービスも一緒にインストール:

```powershell
$env:INSTALL_SERVICE = "true"; irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.ps1 | iex
```

または、インストール後に手動で:

```powershell
etc-scraper.exe -service install   # サービス登録
etc-scraper.exe -service start     # サービス開始
```

### GitHub Releaseから手動インストール

1. [Releases](https://github.com/yhonda-ohishi-pub-dev/scrape-vm/releases) から最新版をダウンロード
2. zipを展開して `etc-scraper.exe` をPATHの通った場所に配置

### ソースからビルド

```powershell
# 要件: Go 1.24以上
go mod download
go build -o etc-scraper.exe .
```

## 使用方法

### CLIモード

```bash
# 単一アカウント
./etc-scraper -accounts=user1:pass1

# 複数アカウント
./etc-scraper -accounts=user1:pass1,user2:pass2

# 環境変数から読み込み
export ETC_CORP_ACCOUNTS="user1:pass1,user2:pass2"
./etc-scraper

# JSON配列形式も対応
export ETC_CORP_ACCOUNTS='["user1:pass1","user2:pass2"]'
./etc-scraper
```

### gRPCサーバーモード

```bash
# サーバー起動
./etc-scraper -grpc -port=50051

# grpcurl でテスト
grpcurl -plaintext -d '{"user_id":"xxx","password":"xxx"}' localhost:50051 scraper.ETCScraper/Scrape
```

### P2Pモード（WebRTC）

ブラウザからWebRTC経由で直接制御できるモード。Cloudflare Workersのシグナリングサーバーを使用。

```bash
# 初回セットアップ（OAuth認証でAPIキー取得）
./etc-scraper -p2p-setup

# P2Pモードで起動（クレデンシャルファイルから自動読み込み）
./etc-scraper -p2p

# APIキーを直接指定する場合
./etc-scraper -p2p -p2p-apikey=YOUR_API_KEY

# 環境変数で指定する場合
P2P_API_KEY=xxx ./etc-scraper -p2p
```

#### P2Pコマンド（ブラウザから送信）

| コマンド | 説明 |
|----------|------|
| `{"type":"ping"}` | 接続確認 |
| `{"type":"health"}` | バージョン・ステータス取得 |
| `{"type":"scrape","payload":{"accounts":[{"userId":"x","password":"x"}]}}` | スクレイピング実行 |
| `{"type":"get_files"}` | ダウンロード済みファイル取得 |

### オプション

| フラグ | デフォルト | 説明 |
|--------|------------|------|
| `-accounts` | - | アカウント（user:pass形式、カンマ区切り） |
| `-headless` | true | ヘッドレスモードで実行 |
| `-download` | ./downloads | ダウンロードディレクトリ |
| `-grpc` | false | gRPCサーバーモードで起動 |
| `-port` | 50051 | gRPCサーバーポート |
| `-p2p` | false | P2Pモードで起動 |
| `-p2p-setup` | false | P2P APIキー取得セットアップ |
| `-p2p-url` | wss://cf-wbrtc-auth... | シグナリングサーバーURL |
| `-p2p-apikey` | - | P2P APIキー（環境変数P2P_API_KEYでも可） |
| `-p2p-creds` | p2p_credentials.env | クレデンシャルファイルパス |

## gRPC API

### サービス定義

```protobuf
service ETCScraper {
  rpc Scrape(ScrapeRequest) returns (ScrapeResponse);
  rpc ScrapeMultiple(ScrapeMultipleRequest) returns (ScrapeMultipleResponse);
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc GetDownloadedFiles(GetDownloadedFilesRequest) returns (GetDownloadedFilesResponse);
}
```

### RPC説明

| RPC | 説明 |
|-----|------|
| `Scrape` | 単一アカウントのスクレイピング |
| `ScrapeMultiple` | 複数アカウントの非同期スクレイピング（即座にレスポンス返却） |
| `Health` | ヘルスチェック |
| `GetDownloadedFiles` | 最新セッションのダウンロード済みCSVファイルを取得 |

詳細は [proto/scraper.proto](proto/scraper.proto) を参照。

## デプロイ

### GCP VMへのデプロイ

```bash
# Makefileを使用
make deploy

# または PowerShellスクリプト
./deploy.ps1
```

### Make コマンド

```bash
make deploy    # ビルドしてVMにデプロイ
make upload    # ビルドしてアップロードのみ
make build     # Linux/Windows両方ビルド
make ssh       # VMにSSH接続
make clean     # バイナリ削除
```

## ディレクトリ構造

```
scrape-vm/
├── main.go              # エントリーポイント
├── scrapers/
│   ├── base.go          # 共通インターフェース・型定義
│   └── etc.go           # ETCスクレイパー実装
├── server/
│   └── grpc.go          # gRPCサーバー実装
├── proto/
│   ├── scraper.proto    # gRPC定義
│   ├── scraper.pb.go    # 生成コード
│   └── scraper_grpc.pb.go
├── p2p/
│   ├── signaling.go     # WebSocketシグナリングクライアント
│   ├── webrtc.go        # WebRTCクライアント（pion/webrtc）
│   ├── client.go        # 統合P2Pクライアント
│   └── setup.go         # OAuth認証セットアップ
├── Makefile             # ビルド・デプロイ
├── deploy.ps1           # Windows用デプロイスクリプト
├── deploy.sh            # Linux用デプロイスクリプト
├── gcloud.ps1           # gcloudラッパースクリプト
├── etc-scraper.service  # systemdサービス定義
└── downloads/           # CSVダウンロード先
```

## バージョン確認・更新

```powershell
# 現在のバージョン確認
etc-scraper.exe -version

# 更新チェック
etc-scraper.exe -check-update
```

## アンインストール

```powershell
irm https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/uninstall.ps1 | iex
```

サービスも削除する場合は管理者権限で実行してください。
