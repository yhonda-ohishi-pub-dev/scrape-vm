package updater

import "time"

const (
	// GitHub repository
	RepoOwner = "yhonda-ohishi-pub-dev"
	RepoName  = "scrape-vm"

	// Update check interval (default: 1 hour)
	DefaultCheckInterval = 1 * time.Hour

	// Startup delay before first check (allow service to stabilize)
	StartupDelay = 30 * time.Second
)

// Config holds the updater configuration
type Config struct {
	Owner          string
	Repo           string
	CheckInterval  time.Duration
	CurrentVersion string
}

// DefaultConfig returns a default configuration
func DefaultConfig(version string) *Config {
	return &Config{
		Owner:          RepoOwner,
		Repo:           RepoName,
		CheckInterval:  DefaultCheckInterval,
		CurrentVersion: version,
	}
}

// ParseDuration parses a duration string, returning the default interval on error
func ParseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
