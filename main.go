package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anthropics/cf-wbrtc-auth/go/grpcweb"
	svc "github.com/kardianos/service"
	"github.com/pion/webrtc/v4"
	"github.com/scrape-vm/p2p"
	"github.com/scrape-vm/scrapers"
	"github.com/scrape-vm/server"
	myservice "github.com/scrape-vm/service"
	"github.com/scrape-vm/updater"
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

	// サービス管理フラグ
	serviceCmd := flag.String("service", "", "Service command: install|uninstall|start|stop|restart|status")

	// 自動更新フラグ
	checkUpdate := flag.Bool("check-update", false, "Check for updates and exit")
	autoUpdate := flag.Bool("auto-update", true, "Enable automatic updates")
	updateInterval := flag.String("update-interval", "1h", "Update check interval (e.g., 1h, 30m)")

	// バージョン表示
	showVersion := flag.Bool("version", false, "Show version information")

	flag.Parse()

	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	// バージョン表示
	if *showVersion {
		printVersion()
		return
	}

	// 手動更新チェック
	if *checkUpdate {
		runUpdateCheck(logger)
		return
	}

	// サービスコマンド
	if *serviceCmd != "" {
		prg := &myservice.Program{
			Logger:         logger,
			GRPCPort:       *grpcPort,
			DownloadPath:   *downloadPath,
			Headless:       *headless,
			Version:        Version,
			AutoUpdate:     *autoUpdate,
			UpdateInterval: *updateInterval,
		}

		if err := myservice.RunServiceCommand(*serviceCmd, prg, logger); err != nil {
			log.Fatalf("Service command failed: %v", err)
		}
		return
	}

	// サービスとして起動されているか確認
	if isRunningAsService() {
		runAsService(logger, *grpcPort, *downloadPath, *headless, *autoUpdate, *updateInterval)
		return
	}

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
		// APIキーがなければ自動でセットアップを実行
		if apiKey == "" {
			logger.Println("No API key found, starting OAuth setup...")
			apiKey = runAutoSetup(logger, *p2pServerURL, *p2pCredsFile)
			if apiKey == "" {
				log.Fatal("Failed to obtain API key")
			}
		}
		runP2PMode(logger, *p2pURL, apiKey, *p2pAppName, *downloadPath, *headless)
		return
	}

	// gRPCモード
	if *grpcMode {
		runGRPCServerWithAutoUpdate(logger, *grpcPort, *downloadPath, *headless, *autoUpdate, *updateInterval)
		return
	}

	// CLIモード（従来の動作）
	runCLIMode(logger, *accountsFlag, *downloadPath, *headless)
}

// printVersion prints version information
func printVersion() {
	fmt.Printf("etc-scraper version %s\n", Version)
	fmt.Printf("Git commit: %s\n", GitCommit)
	fmt.Printf("Build time: %s\n", BuildTime)
}

// isRunningAsService checks if the process is running as a Windows service
func isRunningAsService() bool {
	return !svc.Interactive()
}

// runAsService runs the application as a Windows service
func runAsService(logger *log.Logger, port, downloadPath string, headless, autoUpdate bool, updateInterval string) {
	prg := &myservice.Program{
		Logger:         logger,
		GRPCPort:       port,
		DownloadPath:   downloadPath,
		Headless:       headless,
		Version:        Version,
		AutoUpdate:     autoUpdate,
		UpdateInterval: updateInterval,
	}

	if err := myservice.RunServiceCommand("run", prg, logger); err != nil {
		log.Fatalf("Service run failed: %v", err)
	}
}

// runGRPCServerWithAutoUpdate runs gRPC server with auto-update support
func runGRPCServerWithAutoUpdate(logger *log.Logger, port, downloadPath string, headless, autoUpdate bool, updateInterval string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if autoUpdate {
		cfg := updater.DefaultConfig(Version)
		if interval, err := updater.ParseDuration(updateInterval); err == nil {
			cfg.CheckInterval = interval
		}

		u := updater.New(cfg, logger)

		// Check for updates at startup
		if updated, err := u.CheckAndUpdate(ctx); err != nil {
			logger.Printf("Update check error: %v", err)
		} else if updated {
			logger.Println("Update applied, restarting...")
			if err := updater.RestartSelf(logger); err != nil {
				logger.Printf("Failed to restart: %v", err)
			}
			return
		}

		// Start periodic update checks
		u.StartPeriodicCheck(ctx, func() {
			logger.Println("Update available, applying...")
			if _, err := u.CheckAndUpdate(ctx); err == nil {
				logger.Println("Update applied, restarting...")
				updater.RestartSelf(logger)
			}
		})
	}

	// Start gRPC server
	server.RunGRPCServer(logger, port, downloadPath, headless)
}

// runUpdateCheck checks for updates and prints the result
func runUpdateCheck(logger *log.Logger) {
	u := updater.New(updater.DefaultConfig(Version), logger)

	ctx := context.Background()
	release, needsUpdate, err := u.CheckForUpdate(ctx)
	if err != nil {
		log.Fatalf("Update check failed: %v", err)
	}

	if needsUpdate {
		fmt.Printf("New version available: %s (current: %s)\n", release.Version(), Version)
		fmt.Println("Run with -auto-update=true to automatically update")
	} else {
		fmt.Printf("You are running the latest version (%s)\n", Version)
	}
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
	// gRPC-Web transport handles messages, this is for non-grpc messages (if any)
	h.logger.Printf("Received raw message (%d bytes) - handled by gRPC-Web transport", len(data))
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
		OnDataChannelReady: func(dc *webrtc.DataChannel) {
			logger.Println("DataChannel ready, setting up gRPC-Web transport...")
			setupGRPCWebTransport(dc, logger, downloadPath, headless)
		},
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

// setupGRPCWebTransport sets up gRPC-Web handlers on the DataChannel
func setupGRPCWebTransport(dc *webrtc.DataChannel, logger *log.Logger, downloadPath string, headless bool) {
	transport := grpcweb.NewTransport(dc, nil)

	// Register Server Reflection
	grpcweb.RegisterReflection(transport)

	// Register scraper.ETCScraper/Health handler
	transport.RegisterHandler("/scraper.ETCScraper/Health", grpcweb.MakeHandler(
		func(data []byte) (json.RawMessage, error) {
			return data, nil
		},
		func(resp map[string]interface{}) ([]byte, error) {
			return json.Marshal(resp)
		},
		func(ctx context.Context, req json.RawMessage) (map[string]interface{}, error) {
			return map[string]interface{}{
				"status":  "ok",
				"version": server.Version,
			}, nil
		},
	))

	// Register scraper.ETCScraper/ScrapeMultiple handler
	transport.RegisterHandler("/scraper.ETCScraper/ScrapeMultiple", grpcweb.MakeHandler(
		func(data []byte) (*ScrapeRequest, error) {
			var req ScrapeRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, err
			}
			return &req, nil
		},
		func(resp *ScrapeResponse) ([]byte, error) {
			return json.Marshal(resp)
		},
		func(ctx context.Context, req *ScrapeRequest) (*ScrapeResponse, error) {
			logger.Printf("Received ScrapeMultiple request with %d accounts", len(req.Accounts))

			// Run scraping in background
			go runScrapeJob(logger, req.Accounts, downloadPath, headless)

			return &ScrapeResponse{
				Message:      "Scraping started",
				AccountCount: len(req.Accounts),
			}, nil
		},
	))

	// Register scraper.ETCScraper/GetDownloadedFiles handler
	transport.RegisterHandler("/scraper.ETCScraper/GetDownloadedFiles", grpcweb.MakeHandler(
		func(data []byte) (json.RawMessage, error) {
			return data, nil
		},
		func(resp *FilesResponse) ([]byte, error) {
			return json.Marshal(resp)
		},
		func(ctx context.Context, req json.RawMessage) (*FilesResponse, error) {
			files, sessionFolder := getDownloadedFiles(downloadPath, logger)
			return &FilesResponse{
				SessionFolder: sessionFolder,
				Files:         files,
			}, nil
		},
	))

	// Start the transport
	transport.Start()
	logger.Println("gRPC-Web transport started")
}

// ScrapeRequest for gRPC-Web
type ScrapeRequest struct {
	Accounts []struct {
		UserID   string `json:"userId"`
		Password string `json:"password"`
	} `json:"accounts"`
}

// ScrapeResponse for gRPC-Web
type ScrapeResponse struct {
	Message      string `json:"message"`
	AccountCount int    `json:"accountCount"`
}

// FilesResponse for gRPC-Web
type FilesResponse struct {
	SessionFolder string                   `json:"sessionFolder"`
	Files         []map[string]interface{} `json:"files"`
}

// runScrapeJob runs scraping in background
func runScrapeJob(logger *log.Logger, accounts []struct {
	UserID   string `json:"userId"`
	Password string `json:"password"`
}, downloadPath string, headless bool) {
	sessionFolder := filepath.Join(downloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		logger.Printf("Failed to create session folder: %v", err)
		return
	}

	successCount := 0
	for i, acc := range accounts {
		logger.Printf("Processing account %d/%d: %s", i+1, len(accounts), acc.UserID)

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

		if i < len(accounts)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	logger.Printf("Scraping completed: %d/%d accounts succeeded", successCount, len(accounts))
}

// runAutoSetup performs OAuth setup and returns API key (for automatic setup during -p2p mode)
func runAutoSetup(logger *log.Logger, serverURL, credsFile string) string {
	logger.Printf("Starting automatic OAuth setup...")
	logger.Printf("Server: %s", serverURL)

	ctx := context.Background()
	result, err := p2p.Setup(ctx, p2p.SetupConfig{
		ServerURL:    serverURL,
		PollInterval: 2 * time.Second,
		Timeout:      5 * time.Minute,
	})
	if err != nil {
		logger.Printf("Setup failed: %v", err)
		return ""
	}

	// 保存
	if err := p2p.SaveCredentials(credsFile, result); err != nil {
		logger.Printf("Warning: Failed to save credentials: %v", err)
	} else {
		logger.Printf("Credentials saved to: %s", credsFile)
	}

	logger.Printf("Setup complete! API Key obtained.")
	return result.APIKey
}

// runP2PSetup runs OAuth setup to get API key (standalone mode)
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
