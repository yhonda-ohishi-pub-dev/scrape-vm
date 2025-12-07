package scrapers

import (
	"context"
	"log"
	"time"
)

// ScraperConfig holds common configuration for all scrapers
type ScraperConfig struct {
	UserID       string
	Password     string
	DownloadPath string
	Headless     bool
	Timeout      time.Duration
}

// ScraperResult represents the result of a scraping operation
type ScraperResult struct {
	Success  bool
	Message  string
	FilePath string
	Content  []byte
}

// Scraper is the interface that all scrapers must implement
type Scraper interface {
	// Initialize sets up the browser and prepares for scraping
	Initialize() error
	// Login performs authentication
	Login() error
	// Download performs the main scraping/download operation
	Download() (string, error)
	// Close cleans up resources
	Close() error
}

// Account represents a user account for scraping
type Account struct {
	UserID   string
	Password string
}

// ProcessAccount processes a single account using the provided scraper factory
func ProcessAccount(config *ScraperConfig, logger *log.Logger, factory func(*ScraperConfig, *log.Logger) (Scraper, error)) (string, error) {
	scraper, err := factory(config, logger)
	if err != nil {
		return "", err
	}
	defer scraper.Close()

	if err := scraper.Initialize(); err != nil {
		return "", err
	}

	if err := scraper.Login(); err != nil {
		return "", err
	}

	return scraper.Download()
}

// BaseScraper provides common functionality for all scrapers
type BaseScraper struct {
	Ctx          context.Context
	Cancel       context.CancelFunc
	AllocCancel  context.CancelFunc
	Config       *ScraperConfig
	Logger       *log.Logger
	DownloadDone chan string
	DownloadPath string
}
