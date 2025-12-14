package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/scrape-vm/scrapers"

	pb "github.com/scrape-vm/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServerImpl implements the gRPC service for use within the Windows service
type GRPCServerImpl struct {
	pb.UnimplementedETCScraperServer
	Logger       *log.Logger
	DownloadPath string
	Headless     bool
	Version      string
}

// Health implements the Health RPC
func (s *GRPCServerImpl) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	s.Logger.Println("Health check requested")
	return &pb.HealthResponse{
		Healthy: true,
		Version: s.Version,
	}, nil
}

// GetDownloadedFiles implements the GetDownloadedFiles RPC
func (s *GRPCServerImpl) GetDownloadedFiles(ctx context.Context, req *pb.GetDownloadedFilesRequest) (*pb.GetDownloadedFilesResponse, error) {
	s.Logger.Println("GetDownloadedFiles requested")

	entries, err := os.ReadDir(s.DownloadPath)
	if err != nil {
		return &pb.GetDownloadedFilesResponse{}, nil
	}

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
func (s *GRPCServerImpl) Scrape(ctx context.Context, req *pb.ScrapeRequest) (*pb.ScrapeResponse, error) {
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

	csvContent, _ := os.ReadFile(csvPath)

	return &pb.ScrapeResponse{
		Success:    true,
		Message:    "Scrape completed successfully",
		CsvPath:    csvPath,
		CsvContent: string(csvContent),
	}, nil
}

// ScrapeMultiple implements the ScrapeMultiple RPC (async version)
func (s *GRPCServerImpl) ScrapeMultiple(ctx context.Context, req *pb.ScrapeMultipleRequest) (*pb.ScrapeMultipleResponse, error) {
	s.Logger.Printf("ScrapeMultiple requested for %d accounts (async)", len(req.Accounts))

	sessionFolder := filepath.Join(s.DownloadPath, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		return &pb.ScrapeMultipleResponse{
			Results:      nil,
			SuccessCount: 0,
			TotalCount:   int32(len(req.Accounts)),
		}, nil
	}

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

			if i < len(req.Accounts)-1 {
				time.Sleep(2 * time.Second)
			}
		}
		s.Logger.Printf("ScrapeMultiple completed for session: %s", sessionFolder)
	}()

	return &pb.ScrapeMultipleResponse{
		Results:      nil,
		SuccessCount: 0,
		TotalCount:   int32(len(req.Accounts)),
	}, nil
}

// StreamDownload implements the StreamDownload RPC for streaming file downloads
func (s *GRPCServerImpl) StreamDownload(req *pb.StreamDownloadRequest, stream grpc.ServerStreamingServer[pb.StreamDownloadChunk]) error {
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
