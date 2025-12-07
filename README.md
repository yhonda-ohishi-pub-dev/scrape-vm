# ETC Scraper

ETC利用照会サービス（etc-meisai.jp）から利用明細CSVを自動ダウンロードするスクレイパーツール。

## 機能

- chromedpを使用したheadlessブラウザ自動化
- gRPCサーバーモードでリモートからスクレイピング実行可能
- 複数アカウント対応
- 新しいタブで開くCSVダウンロードに対応

## 要件

- Go 1.24以上
- Google Chrome（headlessモード用）

## インストール

```bash
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

### オプション

| フラグ | デフォルト | 説明 |
|--------|------------|------|
| `-accounts` | - | アカウント（user:pass形式、カンマ区切り） |
| `-headless` | true | ヘッドレスモードで実行 |
| `-download` | ./downloads | ダウンロードディレクトリ |
| `-grpc` | false | gRPCサーバーモードで起動 |
| `-port` | 50051 | gRPCサーバーポート |

## gRPC API

### サービス定義

```protobuf
service ETCScraper {
  rpc Scrape(ScrapeRequest) returns (ScrapeResponse);
  rpc ScrapeMultiple(ScrapeMultipleRequest) returns (ScrapeMultipleResponse);
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

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
├── main.go              # メインアプリケーション
├── proto/
│   ├── scraper.proto    # gRPC定義
│   ├── scraper.pb.go    # 生成コード
│   └── scraper_grpc.pb.go
├── Makefile             # ビルド・デプロイ
├── deploy.ps1           # Windows用デプロイスクリプト
├── deploy.sh            # Linux用デプロイスクリプト
└── downloads/           # CSVダウンロード先
```

## バージョン

現在のバージョン: **1.1.0**

## コミット履歴

- `5ff9fdd` - ETC明細スクレイパー初期実装
