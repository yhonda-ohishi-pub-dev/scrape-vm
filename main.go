package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	flag.Parse()

	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

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
