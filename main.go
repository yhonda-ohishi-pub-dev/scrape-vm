package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	pb "github.com/scrape-vm/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const Version = "1.1.0"

// Account represents an ETC account
type Account struct {
	UserID   string
	Password string
}

// ScraperConfig holds configuration for the scraper
type ScraperConfig struct {
	UserID       string
	Password     string
	DownloadPath string
	Headless     bool
	Timeout      time.Duration
}

// ETCScraper handles web scraping for ETC meisai service
type ETCScraper struct {
	ctx          context.Context
	cancel       context.CancelFunc
	allocCancel  context.CancelFunc
	config       *ScraperConfig
	logger       *log.Logger
	downloadDone chan string
	downloadPath string
}

// GRPCServer implements the gRPC service
type GRPCServer struct {
	pb.UnimplementedETCScraperServer
	logger       *log.Logger
	downloadPath string
	headless     bool
}

func main() {
	// コマンドラインフラグ
	accountsFlag := flag.String("accounts", "", "Accounts in format: user1:pass1,user2:pass2")
	headless := flag.Bool("headless", true, "Run in headless mode")
	downloadPath := flag.String("download", "./downloads", "Download directory")
	grpcMode := flag.Bool("grpc", false, "Run as gRPC server")
	grpcPort := flag.String("port", "50051", "gRPC server port")
	flag.Parse()

	logger := log.New(os.Stdout, "[ETC-SCRAPER] ", log.LstdFlags)

	// gRPCモード
	if *grpcMode {
		runGRPCServer(logger, *grpcPort, *downloadPath, *headless)
		return
	}

	// CLIモード（従来の動作）
	runCLIMode(logger, *accountsFlag, *downloadPath, *headless)
}

// runGRPCServer starts the gRPC server
func runGRPCServer(logger *log.Logger, port, downloadPath string, headless bool) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	server := &GRPCServer{
		logger:       logger,
		downloadPath: downloadPath,
		headless:     headless,
	}
	pb.RegisterETCScraperServer(s, server)
	reflection.Register(s)

	logger.Printf("gRPC server listening on port %s", port)
	logger.Printf("Download path: %s", downloadPath)
	logger.Printf("Headless mode: %v", headless)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// Health implements the Health RPC
func (s *GRPCServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	s.logger.Println("Health check requested")
	return &pb.HealthResponse{
		Healthy: true,
		Version: Version,
	}, nil
}

// GetDownloadedFiles implements the GetDownloadedFiles RPC
func (s *GRPCServer) GetDownloadedFiles(ctx context.Context, req *pb.GetDownloadedFilesRequest) (*pb.GetDownloadedFilesResponse, error) {
	s.logger.Println("GetDownloadedFiles requested")

	// ダウンロードディレクトリ内の最新セッションフォルダを探す
	entries, err := os.ReadDir(s.downloadPath)
	if err != nil {
		return &pb.GetDownloadedFilesResponse{}, nil
	}

	// 最新のフォルダを探す（YYYYMMDD_HHMMSS形式でソート）
	var latestFolder string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsDir() {
			latestFolder = entries[i].Name()
			break
		}
	}

	if latestFolder == "" {
		s.logger.Println("No session folder found")
		return &pb.GetDownloadedFilesResponse{}, nil
	}

	sessionPath := filepath.Join(s.downloadPath, latestFolder)
	s.logger.Printf("Reading files from: %s", sessionPath)

	// セッションフォルダ内のCSVファイルを読み込む
	files, err := os.ReadDir(sessionPath)
	if err != nil {
		return &pb.GetDownloadedFilesResponse{SessionFolder: latestFolder}, nil
	}

	var downloadedFiles []*pb.DownloadedFile
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		filePath := filepath.Join(sessionPath, f.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			s.logger.Printf("Warning: could not read file %s: %v", f.Name(), err)
			continue
		}
		downloadedFiles = append(downloadedFiles, &pb.DownloadedFile{
			Filename: f.Name(),
			Content:  content,
		})
		s.logger.Printf("Added file: %s (%d bytes)", f.Name(), len(content))
	}

	s.logger.Printf("Returning %d files from session %s", len(downloadedFiles), latestFolder)
	return &pb.GetDownloadedFilesResponse{
		Files:         downloadedFiles,
		SessionFolder: latestFolder,
	}, nil
}

// Scrape implements the Scrape RPC
func (s *GRPCServer) Scrape(ctx context.Context, req *pb.ScrapeRequest) (*pb.ScrapeResponse, error) {
	s.logger.Printf("Scrape requested for user: %s", req.UserId)

	sessionFolder := filepath.Join(s.downloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		return &pb.ScrapeResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create session folder: %v", err),
		}, nil
	}

	config := &ScraperConfig{
		UserID:       req.UserId,
		Password:     req.Password,
		DownloadPath: sessionFolder,
		Headless:     s.headless,
		Timeout:      60 * time.Second,
	}

	csvPath, err := processAccountWithResult(config, s.logger)
	if err != nil {
		return &pb.ScrapeResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	// CSVの内容を読み込む
	csvContent, _ := os.ReadFile(csvPath)

	return &pb.ScrapeResponse{
		Success:    true,
		Message:    "Scrape completed successfully",
		CsvPath:    csvPath,
		CsvContent: string(csvContent),
	}, nil
}

// ScrapeMultiple implements the ScrapeMultiple RPC (非同期版)
func (s *GRPCServer) ScrapeMultiple(ctx context.Context, req *pb.ScrapeMultipleRequest) (*pb.ScrapeMultipleResponse, error) {
	s.logger.Printf("ScrapeMultiple requested for %d accounts (async)", len(req.Accounts))

	sessionFolder := filepath.Join(s.downloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		return &pb.ScrapeMultipleResponse{
			Results:      nil,
			SuccessCount: 0,
			TotalCount:   int32(len(req.Accounts)),
		}, nil
	}

	// バックグラウンドでスクレイピング実行
	go func() {
		for i, acc := range req.Accounts {
			s.logger.Printf("Processing account %d/%d: %s", i+1, len(req.Accounts), acc.UserId)

			config := &ScraperConfig{
				UserID:       acc.UserId,
				Password:     acc.Password,
				DownloadPath: sessionFolder,
				Headless:     s.headless,
				Timeout:      60 * time.Second,
			}

			csvPath, err := processAccountWithResult(config, s.logger)
			if err != nil {
				s.logger.Printf("ERROR: Account %s failed: %v", acc.UserId, err)
				continue
			}
			s.logger.Printf("SUCCESS: Account %s -> %s", acc.UserId, csvPath)

			// アカウント間で待機
			if i < len(req.Accounts)-1 {
				time.Sleep(2 * time.Second)
			}
		}
		s.logger.Printf("ScrapeMultiple completed for session: %s", sessionFolder)
	}()

	// 即座にレスポンスを返す
	return &pb.ScrapeMultipleResponse{
		Results:      nil,
		SuccessCount: 0,
		TotalCount:   int32(len(req.Accounts)),
	}, nil
}

// processAccountWithResult processes a single account and returns the CSV path
func processAccountWithResult(config *ScraperConfig, logger *log.Logger) (string, error) {
	scraper, err := NewETCScraper(config, logger)
	if err != nil {
		return "", fmt.Errorf("failed to create scraper: %w", err)
	}
	defer scraper.Close()

	if err := scraper.Initialize(); err != nil {
		return "", fmt.Errorf("failed to initialize: %w", err)
	}

	if err := scraper.Login(); err != nil {
		return "", fmt.Errorf("failed to login: %w", err)
	}

	csvPath, err := scraper.DownloadMeisai()
	if err != nil {
		return "", fmt.Errorf("failed to download: %w", err)
	}

	// ファイル名にアカウント名を付与
	newPath := filepath.Join(config.DownloadPath, config.UserID+"_"+filepath.Base(csvPath))
	if csvPath != newPath {
		if err := os.Rename(csvPath, newPath); err != nil {
			logger.Printf("Warning: could not rename file: %v", err)
		} else {
			csvPath = newPath
		}
	}

	logger.Printf("Downloaded: %s", csvPath)
	return csvPath, nil
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

		config := &ScraperConfig{
			UserID:       acc.UserID,
			Password:     acc.Password,
			DownloadPath: sessionFolder,
			Headless:     headless,
			Timeout:      60 * time.Second,
		}

		if err := processAccount(config, logger); err != nil {
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
func parseAccounts(flagValue string) []Account {
	var accountsStr string

	if flagValue != "" {
		accountsStr = flagValue
	} else {
		accountsStr = os.Getenv("ETC_CORP_ACCOUNTS")
	}

	if accountsStr == "" {
		return nil
	}

	var accounts []Account

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
func parseAccountString(s string) *Account {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	return &Account{
		UserID:   strings.TrimSpace(parts[0]),
		Password: strings.TrimSpace(parts[1]),
	}
}

// processAccount processes a single account
func processAccount(config *ScraperConfig, logger *log.Logger) error {
	_, err := processAccountWithResult(config, logger)
	return err
}

// NewETCScraper creates a new scraper instance
func NewETCScraper(config *ScraperConfig, logger *log.Logger) (*ETCScraper, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)
	}

	return &ETCScraper{
		config:       config,
		logger:       logger,
		downloadDone: make(chan string, 1),
	}, nil
}

// Initialize sets up chromedp browser
func (s *ETCScraper) Initialize() error {
	s.logger.Println("Initializing browser...")

	if err := os.MkdirAll(s.config.DownloadPath, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	absDownloadPath, err := filepath.Abs(s.config.DownloadPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	s.downloadPath = absDownloadPath

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", s.config.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.WindowSize(1920, 1080),
	)

	if s.config.Headless {
		s.logger.Println("Running in HEADLESS mode")
	} else {
		s.logger.Println("Running in VISIBLE mode")
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(s.logger.Printf))

	s.ctx = ctx
	s.cancel = cancel
	s.allocCancel = allocCancel

	// ブラウザ全体でダウンロードを許可（新しいタブでも有効）
	if err := chromedp.Run(s.ctx,
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(absDownloadPath).
			WithEventsEnabled(true),
	); err != nil {
		return fmt.Errorf("failed to set download behavior: %w", err)
	}

	// ブラウザレベルでダウンロードイベントを監視（新しいタブを含む）
	chromedp.ListenBrowser(s.ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *browser.EventDownloadProgress:
			s.logger.Printf("Browser download event: GUID=%s State=%s", e.GUID, e.State)
			if e.State == browser.DownloadProgressStateCompleted {
				s.logger.Printf("Download completed: %s", e.GUID)
				guidFile := filepath.Join(absDownloadPath, e.GUID)
				if _, err := os.Stat(guidFile); err == nil {
					csvFile := guidFile + ".csv"
					os.Rename(guidFile, csvFile)
					s.logger.Printf("Renamed to: %s", csvFile)
					select {
					case s.downloadDone <- csvFile:
					default:
					}
				} else {
					files, _ := filepath.Glob(filepath.Join(absDownloadPath, "*"))
					for _, f := range files {
						if filepath.Base(f) == e.GUID {
							csvFile := f + ".csv"
							os.Rename(f, csvFile)
							select {
							case s.downloadDone <- csvFile:
							default:
							}
							break
						}
					}
				}
			}
		case *target.EventTargetCreated:
			// 新しいタブが作成されたら、そのタブでもダウンロードを許可
			s.logger.Printf("New target created: %s (type: %s)", e.TargetInfo.TargetID, e.TargetInfo.Type)
		}
	})

	// ターゲットレベルのイベント（ダイアログ等）
	chromedp.ListenTarget(s.ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			s.logger.Printf("Dialog: %s", e.Message)
			go chromedp.Run(s.ctx, page.HandleJavaScriptDialog(true))
		}
	})

	s.logger.Printf("Browser initialized. Download path: %s", absDownloadPath)
	return nil
}

// Login performs login to ETC meisai service
func (s *ETCScraper) Login() error {
	s.logger.Println("Navigating to https://www.etc-meisai.jp/")

	if err := chromedp.Run(s.ctx,
		chromedp.Navigate("https://www.etc-meisai.jp/"),
		chromedp.WaitReady("body"),
	); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	s.logger.Println("Clicking login link...")
	if err := chromedp.Run(s.ctx,
		chromedp.WaitVisible(`a[href*='funccode=1013000000']`),
		chromedp.Click(`a[href*='funccode=1013000000']`),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return fmt.Errorf("failed to click login link: %w", err)
	}

	s.logger.Printf("Filling credentials for user: %s", s.config.UserID)
	if err := chromedp.Run(s.ctx,
		chromedp.WaitVisible(`input[name='risLoginId']`),
		chromedp.SendKeys(`input[name='risLoginId']`, s.config.UserID),
		chromedp.SendKeys(`input[name='risPassword']`, s.config.Password),
	); err != nil {
		return fmt.Errorf("failed to fill credentials: %w", err)
	}

	s.logger.Println("Clicking login button...")
	if err := chromedp.Run(s.ctx,
		chromedp.Click(`input[type='button'][value='ログイン']`),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return fmt.Errorf("failed to click login: %w", err)
	}

	s.logger.Println("Login completed!")
	return nil
}

// DownloadMeisai downloads ETC meisai CSV
func (s *ETCScraper) DownloadMeisai() (string, error) {
	s.logger.Println("Starting download process...")

	s.logger.Println("Navigating to search page...")
	if err := chromedp.Run(s.ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				for (var i = 0; i < links.length; i++) {
					if (links[i].textContent.indexOf('検索条件の指定') >= 0) {
						links[i].click();
						return true;
					}
				}
				return false;
			})()
		`, nil),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		s.logger.Printf("Warning: %v", err)
	}

	s.logger.Println("Selecting '全て' option...")
	chromedp.Run(s.ctx,
		chromedp.Click(`input[name='sokoKbn'][value='0']`, chromedp.NodeVisible),
		chromedp.Sleep(1*time.Second),
	)

	s.logger.Println("Saving settings...")
	chromedp.Run(s.ctx,
		chromedp.Click(`input[name='focusTarget_Save']`, chromedp.NodeVisible),
		chromedp.Sleep(2*time.Second),
	)

	s.logger.Println("Clicking search button...")
	if err := chromedp.Run(s.ctx,
		chromedp.Click(`input[name='focusTarget']`, chromedp.NodeVisible),
		chromedp.Sleep(3*time.Second),
		// ページが完全に読み込まれるまで待つ
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}

	// JavaScriptが完全に読み込まれるまでポーリングで待つ
	s.logger.Println("Waiting for page scripts to load...")
	for i := 0; i < 30; i++ { // 最大30秒待つ
		var ready bool
		chromedp.Run(s.ctx,
			chromedp.Evaluate(`
				(typeof goOutput === 'function' && typeof submitOpenPage === 'function')
			`, &ready),
		)
		if ready {
			s.logger.Println("All scripts loaded!")
			break
		}
		s.logger.Printf("Waiting for scripts... (%d/30)", i+1)
		time.Sleep(1 * time.Second)
	}

	// ページ上のリンクをデバッグ出力
	var allLinks string
	chromedp.Run(s.ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				var texts = [];
				for (var i = 0; i < links.length; i++) {
					texts.push(links[i].textContent.trim());
				}
				return texts.join(' | ');
			})()
		`, &allLinks),
	)
	s.logger.Printf("All links on page: %s", allLinks)

	s.logger.Println("Clicking CSV download link...")

	// CSVリンクをクリック
	var found bool
	chromedp.Run(s.ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				for (var i = 0; i < links.length; i++) {
					var text = links[i].textContent;
					if (text.indexOf('明細') >= 0 && (text.indexOf('CSV') >= 0 || text.indexOf('ＣＳＶ') >= 0)) {
						console.log('Found CSV link: ' + text);
						links[i].click();
						return true;
					}
				}
				return false;
			})()
		`, &found),
	)
	s.logger.Printf("CSV link clicked: %v", found)

	// ダウンロード完了をポーリングで待つ（最大30秒）
	s.logger.Println("Waiting for download...")
	for i := 0; i < 30; i++ {
		select {
		case path := <-s.downloadDone:
			s.logger.Printf("Downloaded (event): %s", path)
			return path, nil
		default:
		}

		// ファイルが存在するかチェック
		allFiles, _ := filepath.Glob(filepath.Join(s.downloadPath, "*"))
		for _, f := range allFiles {
			info, err := os.Stat(f)
			if err != nil || info.IsDir() {
				continue
			}
			// .csvファイルがあれば完了
			if filepath.Ext(f) == ".csv" {
				s.logger.Printf("Found CSV file: %s", f)
				return f, nil
			}
			// 拡張子がないファイル（GUID形式）で十分なサイズがあれば完了
			if filepath.Ext(f) == "" && info.Size() > 100 {
				csvFile := f + ".csv"
				if err := os.Rename(f, csvFile); err == nil {
					s.logger.Printf("Renamed GUID file to: %s", csvFile)
					return csvFile, nil
				}
			}
		}

		time.Sleep(1 * time.Second)
	}

	return "", fmt.Errorf("download timeout")
}

// Close cleans up resources
func (s *ETCScraper) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
	return nil
}
