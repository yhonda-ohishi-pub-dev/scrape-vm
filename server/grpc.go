package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/scrape-vm/scrapers"

	pb "github.com/scrape-vm/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// Version is set from main package at startup
var Version = "dev"

// JobStatusGetter is a function type that returns current job status
type JobStatusGetter func() (*pb.JobStatus, string)

// GRPCServer implements the gRPC service
type GRPCServer struct {
	pb.UnimplementedETCScraperServer
	Logger            *log.Logger
	DownloadPath      string
	Headless          bool
	GetJobStatus      JobStatusGetter
	lastSessionFolder string

	// Internal job state for gRPC mode
	jobMu             sync.RWMutex
	isRunning         bool
	startedAt         time.Time
	totalAccounts     int
	completedAccounts int
	successCount      int
	failCount         int
	currentAccount    string
	lastError         string
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
	resp := &pb.HealthResponse{
		Healthy:           true,
		Version:           Version,
		LastSessionFolder: s.lastSessionFolder,
	}
	if s.GetJobStatus != nil {
		jobStatus, sessionFolder := s.GetJobStatus()
		resp.CurrentJob = jobStatus
		if sessionFolder != "" {
			resp.LastSessionFolder = sessionFolder
		}
	} else {
		// Use internal job state
		resp.CurrentJob = s.getInternalJobStatus()
	}
	return resp, nil
}

// getInternalJobStatus returns the internal job status
func (s *GRPCServer) getInternalJobStatus() *pb.JobStatus {
	s.jobMu.RLock()
	defer s.jobMu.RUnlock()
	return &pb.JobStatus{
		IsRunning:         s.isRunning,
		StartedAt:         s.startedAt.Format(time.RFC3339),
		TotalAccounts:     int32(s.totalAccounts),
		CompletedAccounts: int32(s.completedAccounts),
		SuccessCount:      int32(s.successCount),
		FailCount:         int32(s.failCount),
		CurrentAccount:    s.currentAccount,
		LastError:         s.lastError,
	}
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

	// Start job tracking BEFORE launching goroutine
	s.startJob(len(req.Accounts), sessionFolder)

	// バックグラウンドでスクレイピング実行
	go func() {
		defer s.finishJob()

		for i, acc := range req.Accounts {
			s.Logger.Printf("Processing account %d/%d: %s", i+1, len(req.Accounts), acc.UserId)
			s.setCurrentAccount(acc.UserId)

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
				s.accountFailed(err.Error())
				continue
			}
			s.Logger.Printf("SUCCESS: Account %s -> %s", acc.UserId, csvPath)
			s.accountSuccess()

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

// Job state management methods
func (s *GRPCServer) startJob(totalAccounts int, sessionFolder string) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.isRunning = true
	s.startedAt = time.Now()
	s.totalAccounts = totalAccounts
	s.completedAccounts = 0
	s.successCount = 0
	s.failCount = 0
	s.currentAccount = ""
	s.lastError = ""
	s.lastSessionFolder = sessionFolder
}

func (s *GRPCServer) setCurrentAccount(account string) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if len(account) > 4 {
		s.currentAccount = account[:4] + "****"
	} else {
		s.currentAccount = account
	}
}

func (s *GRPCServer) accountSuccess() {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.completedAccounts++
	s.successCount++
	s.currentAccount = ""
}

func (s *GRPCServer) accountFailed(errMsg string) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.completedAccounts++
	s.failCount++
	s.lastError = errMsg
	s.currentAccount = ""
}

func (s *GRPCServer) finishJob() {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.isRunning = false
	s.currentAccount = ""
}

// StreamDownload implements the StreamDownload RPC for streaming file downloads
func (s *GRPCServer) StreamDownload(req *pb.StreamDownloadRequest, stream grpc.ServerStreamingServer[pb.StreamDownloadChunk]) error {
	s.Logger.Println("StreamDownload requested")

	// セッションフォルダを決定
	sessionFolder := req.GetSessionFolder()
	if sessionFolder == "" {
		// 最新のセッションフォルダを使用
		entries, err := os.ReadDir(s.DownloadPath)
		if err != nil {
			return status.Errorf(codes.NotFound, "failed to read download path: %v", err)
		}

		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				sessionFolder = filepath.Join(s.DownloadPath, entries[i].Name())
				break
			}
		}

		if sessionFolder == "" {
			return status.Error(codes.NotFound, "no session folder found")
		}
	}

	s.Logger.Printf("Streaming files from: %s", sessionFolder)

	// フォルダ内のファイル一覧を取得
	files, err := os.ReadDir(sessionFolder)
	if err != nil {
		return status.Errorf(codes.NotFound, "session folder not found: %v", err)
	}

	// ファイル以外を除外してカウント
	var fileList []os.DirEntry
	for _, f := range files {
		if !f.IsDir() {
			fileList = append(fileList, f)
		}
	}

	totalFiles := int32(len(fileList))
	if totalFiles == 0 {
		s.Logger.Println("No files found in session folder")
		return nil
	}

	const chunkSize = 64 * 1024 // 64KB chunks

	for fileIndex, file := range fileList {
		filePath := filepath.Join(sessionFolder, file.Name())
		fileInfo, err := file.Info()
		if err != nil {
			s.Logger.Printf("Warning: could not get file info for %s: %v", file.Name(), err)
			continue
		}

		totalSize := fileInfo.Size()
		s.Logger.Printf("Streaming file %d/%d: %s (%d bytes)", fileIndex+1, totalFiles, file.Name(), totalSize)

		f, err := os.Open(filePath)
		if err != nil {
			s.Logger.Printf("Warning: could not open file %s: %v", file.Name(), err)
			continue
		}

		buf := make([]byte, chunkSize)
		var offset int64 = 0

		for {
			n, err := f.Read(buf)
			if n > 0 {
				isLastChunk := err == io.EOF || offset+int64(n) >= totalSize

				chunk := &pb.StreamDownloadChunk{
					Filename:    file.Name(),
					Data:        buf[:n],
					Offset:      offset,
					TotalSize:   totalSize,
					IsLastChunk: isLastChunk,
					FileIndex:   int32(fileIndex),
					TotalFiles:  totalFiles,
				}

				if sendErr := stream.Send(chunk); sendErr != nil {
					f.Close()
					return status.Errorf(codes.Internal, "failed to send chunk: %v", sendErr)
				}

				offset += int64(n)
			}

			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return status.Errorf(codes.Internal, "failed to read file %s: %v", file.Name(), err)
			}
		}

		f.Close()
		s.Logger.Printf("Completed streaming file: %s", file.Name())
	}

	s.Logger.Printf("StreamDownload completed: %d files sent", totalFiles)
	return nil
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
