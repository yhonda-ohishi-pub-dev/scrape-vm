package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/scrape-vm/scrapers"

	pb "github.com/scrape-vm/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const Version = "1.2.0"

// GRPCServer implements the gRPC service
type GRPCServer struct {
	pb.UnimplementedETCScraperServer
	Logger       *log.Logger
	DownloadPath string
	Headless     bool
}

// RunGRPCServer starts the gRPC server
func RunGRPCServer(logger *log.Logger, port, downloadPath string, headless bool) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	server := &GRPCServer{
		Logger:       logger,
		DownloadPath: downloadPath,
		Headless:     headless,
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
	s.Logger.Println("Health check requested")
	return &pb.HealthResponse{
		Healthy: true,
		Version: Version,
	}, nil
}

// GetDownloadedFiles implements the GetDownloadedFiles RPC
func (s *GRPCServer) GetDownloadedFiles(ctx context.Context, req *pb.GetDownloadedFilesRequest) (*pb.GetDownloadedFilesResponse, error) {
	s.Logger.Println("GetDownloadedFiles requested")

	// ダウンロードディレクトリ内の最新セッションフォルダを探す
	entries, err := os.ReadDir(s.DownloadPath)
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
		s.Logger.Println("No session folder found")
		return &pb.GetDownloadedFilesResponse{}, nil
	}

	sessionPath := filepath.Join(s.DownloadPath, latestFolder)
	s.Logger.Printf("Reading files from: %s", sessionPath)

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
			s.Logger.Printf("Warning: could not read file %s: %v", f.Name(), err)
			continue
		}
		downloadedFiles = append(downloadedFiles, &pb.DownloadedFile{
			Filename: f.Name(),
			Content:  content,
		})
		s.Logger.Printf("Added file: %s (%d bytes)", f.Name(), len(content))
	}

	s.Logger.Printf("Returning %d files from session %s", len(downloadedFiles), latestFolder)
	return &pb.GetDownloadedFilesResponse{
		Files:         downloadedFiles,
		SessionFolder: latestFolder,
	}, nil
}

// Scrape implements the Scrape RPC
func (s *GRPCServer) Scrape(ctx context.Context, req *pb.ScrapeRequest) (*pb.ScrapeResponse, error) {
	s.Logger.Printf("Scrape requested for user: %s", req.UserId)

	sessionFolder := filepath.Join(s.DownloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		return &pb.ScrapeResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create session folder: %v", err),
		}, nil
	}

	config := &scrapers.ScraperConfig{
		UserID:       req.UserId,
		Password:     req.Password,
		DownloadPath: sessionFolder,
		Headless:     s.Headless,
		Timeout:      60 * time.Second,
	}

	csvPath, err := processETCAccountWithResult(config, s.Logger)
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
	s.Logger.Printf("ScrapeMultiple requested for %d accounts (async)", len(req.Accounts))

	sessionFolder := filepath.Join(s.DownloadPath, time.Now().Format("20060102_150405"))
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
			s.Logger.Printf("Processing account %d/%d: %s", i+1, len(req.Accounts), acc.UserId)

			config := &scrapers.ScraperConfig{
				UserID:       acc.UserId,
				Password:     acc.Password,
				DownloadPath: sessionFolder,
				Headless:     s.Headless,
				Timeout:      60 * time.Second,
			}

			csvPath, err := processETCAccountWithResult(config, s.Logger)
			if err != nil {
				s.Logger.Printf("ERROR: Account %s failed: %v", acc.UserId, err)
				continue
			}
			s.Logger.Printf("SUCCESS: Account %s -> %s", acc.UserId, csvPath)

			// アカウント間で待機
			if i < len(req.Accounts)-1 {
				time.Sleep(2 * time.Second)
			}
		}
		s.Logger.Printf("ScrapeMultiple completed for session: %s", sessionFolder)
	}()

	// 即座にレスポンスを返す
	return &pb.ScrapeMultipleResponse{
		Results:      nil,
		SuccessCount: 0,
		TotalCount:   int32(len(req.Accounts)),
	}, nil
}

// processETCAccountWithResult processes a single ETC account and returns the CSV path
func processETCAccountWithResult(config *scrapers.ScraperConfig, logger *log.Logger) (string, error) {
	scraper, err := scrapers.NewETCScraper(config, logger)
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

	csvPath, err := scraper.Download()
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
