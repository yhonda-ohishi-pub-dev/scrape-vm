package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/scrape-vm/p2p"
	"github.com/scrape-vm/scrapers"
	"github.com/scrape-vm/server"
)

func main() {
	// コマンドラインフラグ
	accountsFlag := flag.String("accounts", "", "Accounts in format: user1:pass1,user2:pass2")
	headless := flag.Bool("headless", true, "Run in headless mode")
	downloadPath := flag.String("download", "./downloads", "Download directory")
	grpcMode := flag.Bool("grpc", false, "Run as gRPC server")
	grpcPort := flag.String("port", "50051", "gRPC server port")
	// P2Pモード用フラグ
	p2pMode := flag.Bool("p2p", false, "Run as P2P client")
	p2pSetup := flag.Bool("p2p-setup", false, "Run P2P OAuth setup to get API key")
	p2pURL := flag.String("p2p-url", "wss://cf-wbrtc-auth.m-tama-ramu.workers.dev/ws/app", "P2P signaling server URL")
	p2pServerURL := flag.String("p2p-server", "https://cf-wbrtc-auth.m-tama-ramu.workers.dev", "P2P server base URL for setup")
	p2pAPIKey := flag.String("p2p-apikey", "", "P2P API key (or set P2P_API_KEY env)")
	p2pAppName := flag.String("p2p-name", "etc-scraper", "P2P app name")
	p2pCredsFile := flag.String("p2p-creds", "p2p_credentials.env", "P2P credentials file path")
	flag.Parse()

	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	// P2Pセットアップモード（APIキー取得）
	if *p2pSetup {
		runP2PSetup(logger, *p2pServerURL, *p2pCredsFile)
		return
	}

	// P2Pモード
	if *p2pMode {
		apiKey := *p2pAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("P2P_API_KEY")
		}
		// クレデンシャルファイルから読み込み
		if apiKey == "" {
			if creds, err := p2p.LoadCredentials(*p2pCredsFile); err == nil {
				apiKey = creds.APIKey
				logger.Printf("Loaded API key from %s", *p2pCredsFile)
			}
		}
		if apiKey == "" {
			log.Fatal("P2P mode requires API key. Run with -p2p-setup first, or use -p2p-apikey / P2P_API_KEY env")
		}
		runP2PMode(logger, *p2pURL, apiKey, *p2pAppName, *downloadPath, *headless)
		return
	}

	// gRPCモード
	if *grpcMode {
		server.RunGRPCServer(logger, *grpcPort, *downloadPath, *headless)
		return
	}

	// CLIモード（従来の動作）
	runCLIMode(logger, *accountsFlag, *downloadPath, *headless)
}

// runCLIMode runs the scraper in CLI mode
func runCLIMode(logger *log.Logger, accountsFlag, downloadPath string, headless bool) {
	accounts := parseAccounts(accountsFlag)

	if len(accounts) == 0 {
		log.Fatal("Usage: etc-scraper -accounts=user1:pass1,user2:pass2\n" +
			"Or set ETC_CORP_ACCOUNTS=user1:pass1,user2:pass2\n" +
			"Or set ETC_CORP_ACCOUNTS=[\"user1:pass1\",\"user2:pass2\"]\n" +
			"Or run as gRPC server: etc-scraper -grpc -port=50051")
	}

	logger.Printf("Found %d account(s) to process", len(accounts))

	sessionFolder := filepath.Join(downloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		log.Fatalf("Failed to create session folder: %v", err)
	}
	logger.Printf("Session folder: %s", sessionFolder)

	successCount := 0
	for i, acc := range accounts {
		logger.Printf("=== Processing account %d/%d: %s ===", i+1, len(accounts), acc.UserID)

		config := &scrapers.ScraperConfig{
			UserID:       acc.UserID,
			Password:     acc.Password,
			DownloadPath: sessionFolder,
			Headless:     headless,
			Timeout:      60 * time.Second,
		}

		if err := processETCAccount(config, logger); err != nil {
			logger.Printf("ERROR: Failed to process account %s: %v", acc.UserID, err)
			continue
		}

		successCount++
		logger.Printf("SUCCESS: Account %s completed", acc.UserID)

		if i < len(accounts)-1 {
			logger.Println("Waiting before next account...")
			time.Sleep(2 * time.Second)
		}
	}

	logger.Printf("=== Complete: %d/%d accounts succeeded ===", successCount, len(accounts))
	logger.Printf("CSV files saved to: %s", sessionFolder)
}

// parseAccounts parses account information from flag or environment variable
func parseAccounts(flagValue string) []scrapers.Account {
	var accountsStr string

	if flagValue != "" {
		accountsStr = flagValue
	} else {
		accountsStr = os.Getenv("ETC_CORP_ACCOUNTS")
	}

	if accountsStr == "" {
		return nil
	}

	var accounts []scrapers.Account

	// JSON配列形式をチェック ["user1:pass1","user2:pass2"]
	if strings.HasPrefix(strings.TrimSpace(accountsStr), "[") {
		var jsonAccounts []string
		if err := json.Unmarshal([]byte(accountsStr), &jsonAccounts); err == nil {
			for _, acc := range jsonAccounts {
				if a := parseAccountString(acc); a != nil {
					accounts = append(accounts, *a)
				}
			}
			return accounts
		}
	}

	// カンマ区切り形式 user1:pass1,user2:pass2
	for _, acc := range strings.Split(accountsStr, ",") {
		if a := parseAccountString(acc); a != nil {
			accounts = append(accounts, *a)
		}
	}

	return accounts
}

// parseAccountString parses a single account string "user:pass"
func parseAccountString(s string) *scrapers.Account {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	return &scrapers.Account{
		UserID:   strings.TrimSpace(parts[0]),
		Password: strings.TrimSpace(parts[1]),
	}
}

// processETCAccount processes a single ETC account
func processETCAccount(config *scrapers.ScraperConfig, logger *log.Logger) error {
	scraper, err := scrapers.NewETCScraper(config, logger)
	if err != nil {
		return err
	}
	defer scraper.Close()

	if err := scraper.Initialize(); err != nil {
		return err
	}

	if err := scraper.Login(); err != nil {
		return err
	}

	csvPath, err := scraper.Download()
	if err != nil {
		return err
	}

	// ファイル名にアカウント名を付与
	newPath := filepath.Join(config.DownloadPath, config.UserID+"_"+filepath.Base(csvPath))
	if csvPath != newPath {
		if err := os.Rename(csvPath, newPath); err != nil {
			logger.Printf("Warning: could not rename file: %v", err)
		}
	}

	return nil
}

// p2pEventHandler implements p2p.ClientEventHandler
type p2pEventHandler struct {
	client       *p2p.Client
	logger       *log.Logger
	downloadPath string
	headless     bool
}

func (h *p2pEventHandler) OnP2PConnected() {
	h.logger.Println("Browser connected via WebRTC!")
}

func (h *p2pEventHandler) OnP2PDisconnected() {
	h.logger.Println("Browser disconnected")
}

func (h *p2pEventHandler) OnP2PMessage(data []byte) {
	h.logger.Printf("Received message: %s", string(data))
	handleP2PMessage(h.client, h.logger, data, h.downloadPath, h.headless)
}

func (h *p2pEventHandler) OnP2PError(err error) {
	h.logger.Printf("P2P error: %v", err)
}

// runP2PMode runs as P2P client connected to signaling server
func runP2PMode(logger *log.Logger, wsURL, apiKey, appName, downloadPath string, headless bool) {
	logger.Printf("Starting P2P mode...")
	logger.Printf("Signaling URL: %s", wsURL)
	logger.Printf("App name: %s", appName)

	// イベントハンドラを作成（clientは後で設定）
	handler := &p2pEventHandler{
		logger:       logger,
		downloadPath: downloadPath,
		headless:     headless,
	}

	client := p2p.NewClient(&p2p.ClientConfig{
		SignalingURL: wsURL,
		APIKey:       apiKey,
		AppName:      appName,
		Capabilities: []string{"scrape", "etc"},
		Logger:       logger,
		Handler:      handler,
	})

	// ハンドラにclientを設定
	handler.client = client

	// シグナリングサーバーに接続
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	logger.Printf("Connected to signaling server, appID: %s", client.GetAppID())
	logger.Println("Waiting for browser connection... (Ctrl+C to quit)")

	// シグナル待機
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Println("Shutting down...")
}

// runP2PSetup runs OAuth setup to get API key
func runP2PSetup(logger *log.Logger, serverURL, credsFile string) {
	logger.Printf("Starting P2P OAuth setup...")
	logger.Printf("Server: %s", serverURL)

	ctx := context.Background()
	result, err := p2p.Setup(ctx, p2p.SetupConfig{
		ServerURL:    serverURL,
		PollInterval: 2 * time.Second,
		Timeout:      5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// 保存
	if err := p2p.SaveCredentials(credsFile, result); err != nil {
		log.Fatalf("Failed to save credentials: %v", err)
	}

	logger.Printf("Setup complete!")
	logger.Printf("API Key: %s", result.APIKey)
	logger.Printf("App ID: %s", result.AppID)
	logger.Printf("Credentials saved to: %s", credsFile)
	logger.Println("")
	logger.Println("Now you can run P2P mode:")
	logger.Printf("  ./etc-scraper.exe -p2p")
}

// P2PMessage represents a message from browser
type P2PMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ScrapePayload for scrape command
type ScrapePayload struct {
	Accounts []struct {
		UserID   string `json:"userId"`
		Password string `json:"password"`
	} `json:"accounts"`
}

// P2PResponse represents a response to browser
type P2PResponse struct {
	Type    string      `json:"type"`
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func handleP2PMessage(client *p2p.Client, logger *log.Logger, data []byte, downloadPath string, headless bool) {
	var msg P2PMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Printf("Failed to parse message: %v", err)
		sendP2PResponse(client, "error", false, "Invalid message format", nil)
		return
	}

	switch msg.Type {
	case "ping":
		sendP2PResponse(client, "pong", true, "", nil)

	case "health":
		sendP2PResponse(client, "health", true, "", map[string]interface{}{
			"version": server.Version,
			"status":  "ok",
		})

	case "scrape":
		var payload ScrapePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			sendP2PResponse(client, "scrape_result", false, "Invalid payload", nil)
			return
		}

		// バックグラウンドでスクレイピング実行
		go func() {
			sessionFolder := filepath.Join(downloadPath, time.Now().Format("20060102_150405"))
			if err := os.MkdirAll(sessionFolder, 0755); err != nil {
				sendP2PResponse(client, "scrape_result", false, err.Error(), nil)
				return
			}

			sendP2PResponse(client, "scrape_started", true, "Scraping started", map[string]interface{}{
				"sessionFolder": filepath.Base(sessionFolder),
				"accountCount":  len(payload.Accounts),
			})

			successCount := 0
			for i, acc := range payload.Accounts {
				logger.Printf("Processing account %d/%d: %s", i+1, len(payload.Accounts), acc.UserID)

				config := &scrapers.ScraperConfig{
					UserID:       acc.UserID,
					Password:     acc.Password,
					DownloadPath: sessionFolder,
					Headless:     headless,
					Timeout:      60 * time.Second,
				}

				if err := processETCAccount(config, logger); err != nil {
					logger.Printf("ERROR: %s: %v", acc.UserID, err)
					continue
				}
				successCount++

				if i < len(payload.Accounts)-1 {
					time.Sleep(2 * time.Second)
				}
			}

			sendP2PResponse(client, "scrape_result", true, "Scraping completed", map[string]interface{}{
				"sessionFolder": filepath.Base(sessionFolder),
				"successCount":  successCount,
				"totalCount":    len(payload.Accounts),
			})
		}()

	case "get_files":
		files, sessionFolder := getDownloadedFiles(downloadPath, logger)
		sendP2PResponse(client, "files", true, "", map[string]interface{}{
			"sessionFolder": sessionFolder,
			"files":         files,
		})

	default:
		sendP2PResponse(client, "error", false, "Unknown command: "+msg.Type, nil)
	}
}

func sendP2PResponse(client *p2p.Client, msgType string, success bool, message string, data interface{}) {
	resp := P2PResponse{
		Type:    msgType,
		Success: success,
		Message: message,
		Data:    data,
	}
	if err := client.SendJSON(resp); err != nil {
		log.Printf("Failed to send response: %v", err)
	}
}

func getDownloadedFiles(downloadPath string, logger *log.Logger) ([]map[string]interface{}, string) {
	entries, err := os.ReadDir(downloadPath)
	if err != nil {
		return nil, ""
	}

	var latestFolder string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsDir() {
			latestFolder = entries[i].Name()
			break
		}
	}

	if latestFolder == "" {
		return nil, ""
	}

	sessionPath := filepath.Join(downloadPath, latestFolder)
	files, err := os.ReadDir(sessionPath)
	if err != nil {
		return nil, latestFolder
	}

	var result []map[string]interface{}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		filePath := filepath.Join(sessionPath, f.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"filename": f.Name(),
			"content":  string(content),
			"size":     len(content),
		})
	}

	return result, latestFolder
}
