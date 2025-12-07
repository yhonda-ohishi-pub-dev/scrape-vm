# CLAUDE.md

このファイルはClaude Codeがこのリポジトリで作業する際のガイダンスを提供します。

## プロジェクト概要

ETC利用照会サービス（etc-meisai.jp）から利用明細CSVを自動ダウンロードするGoアプリケーション。CLIモードとgRPCサーバーモードの両方をサポート。

## ビルド・実行コマンド

```bash
# ビルド
go build -o etc-scraper.exe .

# Linux用ビルド
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o etc-scraper-linux .

# 実行（CLIモード）
./etc-scraper.exe -accounts=user:pass -headless=true

# 実行（gRPCサーバー）
./etc-scraper.exe -grpc -port=50051

# VMへデプロイ
make deploy
# または
powershell -ExecutionPolicy Bypass -File deploy.ps1
```

## アーキテクチャ

- **main.go** - 全ロジックを含む単一ファイル構成
  - `ETCScraper` - chromedpを使用したスクレイピングロジック
  - `GRPCServer` - gRPCサービス実装
  - CLIモード用のアカウントパース処理

- **proto/** - gRPC定義とコード生成
  - `scraper.proto` - サービス定義
  - 再生成: `protoc --go_out=. --go-grpc_out=. proto/scraper.proto`

## 重要な実装詳細

### ダウンロード処理
- ETCサイトはCSVを新しいタブで開く
- `browser.SetDownloadBehavior`でブラウザレベルでダウンロード許可
- `chromedp.ListenBrowser`でダウンロードイベントを監視
- ファイルポーリングでダウンロード完了を検出
- セッションごとにタイムスタンプ付きフォルダを作成

### gRPC API
- `ScrapeMultiple`: バックグラウンドで非同期実行、即座にレスポンス返却
- `GetDownloadedFiles`: 最新セッションフォルダからCSVファイルを取得
- セッション管理: `lastSessionFolder`変数で最新のダウンロードフォルダを追跡

### アカウント形式
- カンマ区切り: `user1:pass1,user2:pass2`
- JSON配列: `["user1:pass1","user2:pass2"]`
- 環境変数: `ETC_CORP_ACCOUNTS`

## デプロイ先

- VM名: `instance-20251207-115015`
- ゾーン: `asia-northeast1-b`
- リモートパス: `/opt/etc-scraper/etc-scraper`
- systemdサービス: `etc-scraper.service`（ポート50051）

## 注意事項

- Chromeがインストールされている必要あり
- headless=falseでデバッグ可能
- ダウンロードタイムアウト: 30秒
