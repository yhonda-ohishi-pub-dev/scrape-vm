package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthropics/cf-wbrtc-auth/go/grpcweb"
	"github.com/kardianos/service"
	"github.com/pion/webrtc/v4"
	"github.com/scrape-vm/p2p"
	pb "github.com/scrape-vm/proto"
	"github.com/scrape-vm/scrapers"
	"github.com/scrape-vm/server"
	"github.com/scrape-vm/updater"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Program implements service.Interface for Windows service
type Program struct {
	Logger       *log.Logger
	GRPCPort     string
	DownloadPath string
	Headless     bool
	Version      string

	// Auto-update settings
	AutoUpdate     bool
	UpdateInterval string

	// P2P settings
	P2PMode      bool
	P2PURL       string
	P2PAPIKey    string
	P2PAppName   string
	P2PCredsFile string

	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	grpcServer *grpc.Server
	p2pClient  *p2p.Client
	updater    *updater.Updater
	logFile    *os.File // ログファイルハンドル（サービス終了時にクローズ）
}

// Start is called when the service starts
func (p *Program) Start(s service.Service) error {
	// Windowsイベントログを取得
	svcLogger, _ := s.Logger(nil)

	// ファイルロガーを設定
	if err := p.setupFileLogger(); err != nil {
		if svcLogger != nil {
			svcLogger.Error("Failed to setup file logger: " + err.Error())
		}
	}

	if svcLogger != nil {
		svcLogger.Info("Service starting...")
		svcLogger.Info("P2PMode=" + fmt.Sprintf("%v", p.P2PMode))
		svcLogger.Info("P2PCredsFile=" + p.P2PCredsFile)
	}

	// ログファイルに起動メッセージを書き込み
	if p.Logger != nil {
		p.Logger.Printf("Service Start() called, P2PMode=%v", p.P2PMode)
		p.Logger.Printf("P2PURL=%s, P2PCredsFile=%s", p.P2PURL, p.P2PCredsFile)
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Start the main service loop
	go p.run()

	return nil
}

// Stop is called when the service stops
func (p *Program) Stop(s service.Service) error {
	if p.Logger != nil {
		p.Logger.Println("Service stopping...")
	}
	p.cancel()

	// Stop P2P client
	if p.p2pClient != nil {
		p.p2pClient.Close()
	}

	// Stop gRPC server gracefully
	if p.grpcServer != nil {
		p.grpcServer.GracefulStop()
	}

	p.wg.Wait()
	p.Logger.Println("Service stopped")

	// ログファイルをクローズ
	if p.logFile != nil {
		p.logFile.Close()
	}

	return nil
}

// setupFileLogger sets up file logging for the service
func (p *Program) setupFileLogger() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log dir %s: %w", logDir, err)
	}

	logFile := filepath.Join(logDir, "etc-scraper.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	p.logFile = f
	mw := io.MultiWriter(os.Stdout, f)
	p.Logger = log.New(mw, "[SCRAPER] ", log.LstdFlags)
	return nil
}

// run is the main service loop
func (p *Program) run() {
	p.wg.Add(1)
	defer p.wg.Done()

	// Loggerがnilの場合はlogFileから作成（recoverより先に実行）
	if p.Logger == nil {
		if p.logFile != nil {
			p.Logger = log.New(p.logFile, "[SCRAPER] ", log.LstdFlags)
		} else {
			// 最後の手段: stderrに出力
			p.Logger = log.New(os.Stderr, "[SCRAPER] ", log.LstdFlags)
		}
	}

	// Recover from panic
	defer func() {
		if r := recover(); r != nil {
			if p.Logger != nil {
				p.Logger.Printf("run() panic recovered: %v", r)
			}
		}
	}()

	// Resolve download path to absolute path if relative
	if !filepath.IsAbs(p.DownloadPath) {
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		p.DownloadPath = filepath.Join(exeDir, p.DownloadPath)
	}

	// Ensure download directory exists
	if err := os.MkdirAll(p.DownloadPath, 0755); err != nil {
		p.Logger.Printf("Failed to create download directory: %v", err)
	}

	// Start auto-update if enabled
	if p.AutoUpdate {
		p.startAutoUpdate()
	}

	// Start P2P or gRPC server
	if p.P2PMode {
		p.runP2PClient()
	} else {
		p.runGRPCServer()
	}
}

// startAutoUpdate initializes and starts the auto-updater
func (p *Program) startAutoUpdate() {
	cfg := updater.DefaultConfig(p.Version)
	if p.UpdateInterval != "" {
		if interval, err := updater.ParseDuration(p.UpdateInterval); err == nil {
			cfg.CheckInterval = interval
		}
	}

	p.updater = updater.New(cfg, p.Logger)

	// Check for updates at startup (non-blocking)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.Logger.Printf("Auto-update startup check panic recovered: %v", r)
			}
		}()
		if updated, err := p.updater.CheckAndUpdate(p.ctx); err != nil {
			p.Logger.Printf("Startup update check failed: %v", err)
		} else if updated {
			p.Logger.Println("Update applied, service will restart...")
			if err := updater.RestartService(ServiceName, p.Logger); err != nil {
				p.Logger.Printf("Failed to restart service: %v", err)
			}
		}
	}()

	// Start periodic update checks
	p.updater.StartPeriodicCheck(p.ctx, func() {
		defer func() {
			if r := recover(); r != nil {
				p.Logger.Printf("Auto-update periodic check panic recovered: %v", r)
			}
		}()
		p.Logger.Println("Update available, applying...")
		if _, err := p.updater.CheckAndUpdate(p.ctx); err != nil {
			p.Logger.Printf("Failed to apply update: %v", err)
			return
		}
		p.Logger.Println("Update applied, restarting service...")
		if err := updater.RestartService(ServiceName, p.Logger); err != nil {
			p.Logger.Printf("Failed to restart service: %v", err)
		}
	})
}

// runGRPCServer starts the gRPC server
func (p *Program) runGRPCServer() {
	lis, err := net.Listen("tcp", ":"+p.GRPCPort)
	if err != nil {
		p.Logger.Printf("Failed to listen: %v", err)
		return
	}

	p.grpcServer = grpc.NewServer()
	server := &GRPCServerImpl{
		Logger:       p.Logger,
		DownloadPath: p.DownloadPath,
		Headless:     p.Headless,
		Version:      p.Version,
	}
	pb.RegisterETCScraperServer(p.grpcServer, server)
	reflection.Register(p.grpcServer)

	p.Logger.Printf("gRPC server listening on port %s", p.GRPCPort)
	p.Logger.Printf("Download path: %s", p.DownloadPath)
	p.Logger.Printf("Headless mode: %v", p.Headless)
	p.Logger.Printf("Version: %s", p.Version)

	// Serve until context is cancelled
	go func() {
		<-p.ctx.Done()
		p.grpcServer.GracefulStop()
	}()

	if err := p.grpcServer.Serve(lis); err != nil {
		p.Logger.Printf("gRPC server stopped: %v", err)
	}
}

// runP2PClient starts the P2P client for WebRTC communication
func (p *Program) runP2PClient() {
	// Loggerがnilの場合の安全対策
	if p.Logger == nil {
		if p.logFile != nil {
			p.Logger = log.New(p.logFile, "[SCRAPER] ", log.LstdFlags)
		} else {
			p.Logger = log.New(os.Stderr, "[SCRAPER] ", log.LstdFlags)
		}
	}

	p.Logger.Printf("Starting P2P mode...")
	p.Logger.Printf("Signaling URL: %s", p.P2PURL)
	p.Logger.Printf("App name: %s", p.P2PAppName)

	// Load API key from credentials file if not provided
	apiKey := p.P2PAPIKey
	if apiKey == "" {
		// Try the provided path first
		credsFile := p.P2PCredsFile
		if creds, err := p2p.LoadCredentials(credsFile); err == nil {
			apiKey = creds.APIKey
			p.Logger.Printf("Loaded API key from %s", credsFile)
		} else {
			// Try the executable directory (for Windows service)
			exePath, _ := os.Executable()
			exeDir := filepath.Dir(exePath)
			credsFile = filepath.Join(exeDir, "p2p_credentials.env")
			p.Logger.Printf("Trying credentials from executable dir: %s", credsFile)
			if creds, err := p2p.LoadCredentials(credsFile); err == nil {
				apiKey = creds.APIKey
				p.Logger.Printf("Loaded API key from %s", credsFile)
			} else {
				p.Logger.Printf("Failed to load credentials: %v", err)
				p.Logger.Println("Please run P2P setup first: etc-scraper.exe -p2p-setup")
				p.Logger.Println("Service will keep running and retry periodically...")
				<-p.ctx.Done()
				return
			}
		}
	}

	// Create event handler
	handler := &serviceP2PEventHandler{
		program: p,
	}

	p.p2pClient = p2p.NewClient(&p2p.ClientConfig{
		SignalingURL: p.P2PURL,
		APIKey:       apiKey,
		AppName:      p.P2PAppName,
		Capabilities: []string{"scrape", "etc"},
		Logger:       p.Logger,
		Handler:      handler,
		OnDataChannelReady: func(dc *webrtc.DataChannel) {
			p.Logger.Println("DataChannel ready, setting up gRPC-Web transport...")
			p.setupGRPCWebTransport(dc)
		},
	})

	// Retry loop - never return unless context is cancelled
	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		// Connect to signaling server in a goroutine
		connectDone := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					p.Logger.Printf("P2P connect panic recovered: %v", r)
					connectDone <- fmt.Errorf("panic: %v", r)
				}
			}()
			err := p.p2pClient.Connect(p.ctx)
			connectDone <- err
		}()

		// Wait for connection with timeout
		var connectErr error
		select {
		case connectErr = <-connectDone:
			// Got result (success or error)
		case <-time.After(30 * time.Second):
			connectErr = fmt.Errorf("connection timeout (30s)")
		case <-p.ctx.Done():
			p.Logger.Println("P2P client shutting down...")
			return
		}

		if connectErr != nil {
			p.Logger.Printf("P2P connection failed: %v, retrying in %v...", connectErr, retryDelay)

			// Wait before retry, but check for context cancellation
			select {
			case <-time.After(retryDelay):
				// Increase retry delay (exponential backoff with cap)
				retryDelay = retryDelay * 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				// Recreate P2P client for retry
				p.p2pClient = p2p.NewClient(&p2p.ClientConfig{
					SignalingURL: p.P2PURL,
					APIKey:       apiKey,
					AppName:      p.P2PAppName,
					Capabilities: []string{"scrape", "etc"},
					Logger:       p.Logger,
					Handler:      handler,
					OnDataChannelReady: func(dc *webrtc.DataChannel) {
						p.Logger.Println("DataChannel ready, setting up gRPC-Web transport...")
						p.setupGRPCWebTransport(dc)
					},
				})
				continue
			case <-p.ctx.Done():
				p.Logger.Println("P2P client shutting down...")
				return
			}
		}

		// Connected successfully
		appID := ""
		if p.p2pClient != nil {
			appID = p.p2pClient.GetAppID()
		}

		p.Logger.Printf("Connected to signaling server, appID: %s", appID)
		p.Logger.Println("Waiting for browser connection...")

		// Wait for context cancellation (this keeps the service alive)
		<-p.ctx.Done()
		p.Logger.Println("P2P client shutting down...")
		return
	}
}

// serviceP2PEventHandler implements p2p.ClientEventHandler for service
type serviceP2PEventHandler struct {
	program *Program
}

func (h *serviceP2PEventHandler) OnP2PConnected() {
	h.program.Logger.Println("Browser connected via WebRTC!")
}

func (h *serviceP2PEventHandler) OnP2PDisconnected() {
	h.program.Logger.Println("Browser disconnected")
}

func (h *serviceP2PEventHandler) OnP2PMessage(data []byte) {
	h.program.Logger.Printf("Received raw message (%d bytes) - handled by gRPC-Web transport", len(data))
}

func (h *serviceP2PEventHandler) OnP2PError(err error) {
	h.program.Logger.Printf("P2P error: %v", err)
}

// setupGRPCWebTransport sets up gRPC-Web handlers on the DataChannel
func (p *Program) setupGRPCWebTransport(dc *webrtc.DataChannel) {
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
		func(data []byte) (*p2pScrapeRequest, error) {
			var req p2pScrapeRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, err
			}
			return &req, nil
		},
		func(resp *p2pScrapeResponse) ([]byte, error) {
			return json.Marshal(resp)
		},
		func(ctx context.Context, req *p2pScrapeRequest) (*p2pScrapeResponse, error) {
			p.Logger.Printf("Received ScrapeMultiple request with %d accounts", len(req.Accounts))

			// Run scraping in background
			go p.runScrapeJob(req.Accounts)

			return &p2pScrapeResponse{
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
		func(resp *p2pFilesResponse) ([]byte, error) {
			return json.Marshal(resp)
		},
		func(ctx context.Context, req json.RawMessage) (*p2pFilesResponse, error) {
			files, sessionFolder := p.getDownloadedFiles()
			return &p2pFilesResponse{
				SessionFolder: sessionFolder,
				Files:         files,
			}, nil
		},
	))

	// Start the transport
	transport.Start()
	p.Logger.Println("gRPC-Web transport started")
}

// P2P request/response types
type p2pScrapeRequest struct {
	Accounts []struct {
		UserID   string `json:"userId"`
		Password string `json:"password"`
	} `json:"accounts"`
}

type p2pScrapeResponse struct {
	Message      string `json:"message"`
	AccountCount int    `json:"accountCount"`
}

type p2pFilesResponse struct {
	SessionFolder string                   `json:"sessionFolder"`
	Files         []map[string]interface{} `json:"files"`
}

// runScrapeJob runs scraping in background
func (p *Program) runScrapeJob(accounts []struct {
	UserID   string `json:"userId"`
	Password string `json:"password"`
}) {
	sessionFolder := filepath.Join(p.DownloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		p.Logger.Printf("Failed to create session folder: %v", err)
		return
	}

	successCount := 0
	for i, acc := range accounts {
		p.Logger.Printf("Processing account %d/%d: %s", i+1, len(accounts), acc.UserID)

		config := &scrapers.ScraperConfig{
			UserID:       acc.UserID,
			Password:     acc.Password,
			DownloadPath: sessionFolder,
			Headless:     p.Headless,
			Timeout:      60 * time.Second,
		}

		scraper, err := scrapers.NewETCScraper(config, p.Logger)
		if err != nil {
			p.Logger.Printf("ERROR: %s: %v", acc.UserID, err)
			continue
		}

		if err := scraper.Initialize(); err != nil {
			scraper.Close()
			p.Logger.Printf("ERROR: %s: %v", acc.UserID, err)
			continue
		}

		if err := scraper.Login(); err != nil {
			scraper.Close()
			p.Logger.Printf("ERROR: %s: %v", acc.UserID, err)
			continue
		}

		csvPath, err := scraper.Download()
		scraper.Close()
		if err != nil {
			p.Logger.Printf("ERROR: %s: %v", acc.UserID, err)
			continue
		}

		// Rename file with account name
		newPath := filepath.Join(sessionFolder, acc.UserID+"_"+filepath.Base(csvPath))
		if csvPath != newPath {
			if err := os.Rename(csvPath, newPath); err != nil {
				p.Logger.Printf("Warning: could not rename file: %v", err)
			}
		}

		successCount++

		if i < len(accounts)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	p.Logger.Printf("Scraping completed: %d/%d accounts succeeded", successCount, len(accounts))
}

// getDownloadedFiles returns files from the latest session folder
func (p *Program) getDownloadedFiles() ([]map[string]interface{}, string) {
	entries, err := os.ReadDir(p.DownloadPath)
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

	sessionPath := filepath.Join(p.DownloadPath, latestFolder)
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
