# CLAUDE.md

このファイルはClaude Codeがこのリポジトリで作業する際のガイダンスを提供します。

## プロジェクト概要

ETC利用照会サービス（etc-meisai.jp）から利用明細CSVを自動ダウンロードするGoアプリケーション。CLIモード、gRPCサーバーモード、P2Pモード、Windowsサービスモードをサポート。GitHub Release経由の自動更新機能付き。

## ビルド・実行コマンド

```bash
# ビルド（バージョン情報付き）
make build-windows

# Linux用ビルド
make build-linux

# 実行（CLIモード）
./etc-scraper.exe -accounts=user:pass -headless=true

# 実行（gRPCサーバー）
./etc-scraper.exe -grpc -port=50051

# 実行（P2Pモード）
# 初回: APIキー取得セットアップ（ブラウザでOAuth認証）
./etc-scraper.exe -p2p-setup

# P2Pモードで起動（クレデンシャルファイルから自動読み込み）
./etc-scraper.exe -p2p

# APIキーを直接指定する場合
./etc-scraper.exe -p2p -p2p-apikey=YOUR_API_KEY
# または環境変数で
P2P_API_KEY=xxx ./etc-scraper.exe -p2p

# Windowsサービスとして実行（管理者権限必要）
./etc-scraper.exe -service install   # サービス登録
./etc-scraper.exe -service start     # サービス開始
./etc-scraper.exe -service stop      # サービス停止
./etc-scraper.exe -service uninstall # サービス削除

# バージョン確認・更新チェック
./etc-scraper.exe -version
./etc-scraper.exe -check-update

# 自動更新サービス（別バイナリ）
./etc-scraper-updater.exe -service install   # サービス登録
./etc-scraper-updater.exe -service start     # サービス開始
./etc-scraper-updater.exe -service stop      # サービス停止
./etc-scraper-updater.exe -service uninstall # サービス削除

# VMへデプロイ
make deploy
# または
powershell -ExecutionPolicy Bypass -File deploy.ps1
```

## アーキテクチャ

- **main.go** - エントリーポイント
  - CLIフラグ解析、起動モード判定
  - アカウントパース処理

- **scrapers/** - スクレイパー実装
  - `base.go` - 共通`Scraper`インターフェースと型定義
  - `etc.go` - ETCスクレイパー（chromedp使用）
  - 新しいスクレイパー追加時はここにファイルを追加

- **server/** - サーバー実装
  - `grpc.go` - gRPCサービス実装

- **service/** - Windowsサービス実装
  - `config.go` - サービス設定
  - `program.go` - service.Interface実装（Start/Stop）
  - `manager.go` - サービス管理コマンド
  - `grpc_impl.go` - サービス用gRPC実装

- **updater/** - 自動更新機能（共有ライブラリ）
  - `config.go` - 更新設定（リポジトリ情報、チェック間隔）
  - `updater.go` - GitHub Release更新チェック・ダウンロード
  - `restart.go` - サービス再起動処理

- **cmd/updater/** - 自動更新サービス（別バイナリ）
  - `main.go` - etc-scraper-updater.exe のエントリーポイント
  - メインサービスを監視・更新するWindowsサービス

- **p2p/** - P2P通信実装（WebRTC + Cloudflare Workers）
  - `signaling.go` - WebSocketシグナリングクライアント
  - `webrtc.go` - pion/webrtcを使用したWebRTCクライアント
  - `client.go` - 統合P2Pクライアント
  - `setup.go` - OAuth認証によるAPIキー取得

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

### P2P通信
- シグナリングサーバー: Cloudflare Workers + Durable Objects（cf-wbrtc-auth）
- シグナリングURL: `wss://cf-wbrtc-auth.m-tama-ramu.workers.dev/ws/app`
- WebRTC: pion/webrtcでDataChannel通信
- 認証: APIキーベース（`-p2p-setup`で取得）
- クレデンシャルファイル: `p2p_credentials.env`
- メッセージ形式: JSON（type, payload）
- 対応コマンド: `ping`, `health`, `scrape`, `get_files`

### アカウント形式
- カンマ区切り: `user1:pass1,user2:pass2`
- JSON配列: `["user1:pass1","user2:pass2"]`
- 環境変数: `ETC_CORP_ACCOUNTS`

## デプロイ先

- VM名: `instance-20251207-115015`
- ゾーン: `asia-northeast1-b`
- リモートパス: `/opt/etc-scraper/etc-scraper`
- systemdサービス: `etc-scraper.service`（ポート50051）

### Windowsサービス
- kardianos/serviceライブラリ使用
- サービス名: `etc-scraper`
- 自動起動設定（インストール時）
- 管理者権限が必要
- サービスは`C:\Windows\System32`で動作するため、パスは絶対パス指定推奨

### 自動更新（etc-scraper-updater.exe）
- **別バイナリ方式**: 更新サービスはメインバイナリとは別の実行ファイル
- creativeprojects/go-selfupdateライブラリ使用
- GitHubリポジトリ: `yhonda-ohishi-pub-dev/scrape-vm`
- デフォルトチェック間隔: 1時間
- 更新フロー:
  1. etc-scraper-updater がGitHub Releaseをチェック
  2. 更新があれば etc-scraper サービスを停止
  3. etc-scraper.exe を新バージョンで置換
  4. etc-scraper サービスを再起動
- バイナリ命名規則: `etc-scraper_v1.x.x_windows_amd64.zip`（両バイナリ含む）
- サービス名: `etc-scraper-updater`

## 注意事項

- Chromeがインストールされている必要あり
- headless=falseでデバッグ可能
- ダウンロードタイムアウト: 30秒
- Windowsサービスのinstall/start/stopには管理者権限必要
