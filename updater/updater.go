package updater

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

// Updater handles checking for and applying updates
type Updater struct {
	config *Config
	logger *log.Logger
}

// New creates a new Updater
func New(config *Config, logger *log.Logger) *Updater {
	return &Updater{
		config: config,
		logger: logger,
	}
}

// CheckForUpdate checks if a newer version is available
func (u *Updater) CheckForUpdate(ctx context.Context) (*selfupdate.Release, bool, error) {
	u.logger.Printf("Checking for updates... (current: %s)", u.config.CurrentVersion)

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, false, fmt.Errorf("failed to create GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to create updater: %w", err)
	}

	repository := selfupdate.ParseSlug(fmt.Sprintf("%s/%s", u.config.Owner, u.config.Repo))
	latest, found, err := updater.DetectLatest(ctx, repository)
	if err != nil {
		return nil, false, fmt.Errorf("failed to detect latest version: %w", err)
	}

	if !found {
		u.logger.Printf("No release found for %s/%s", runtime.GOOS, runtime.GOARCH)
		return nil, false, nil
	}

	currentVersion := u.config.CurrentVersion
	// Ensure version starts with 'v' for comparison
	if len(currentVersion) > 0 && currentVersion[0] != 'v' {
		currentVersion = "v" + currentVersion
	}

	if latest.LessOrEqual(currentVersion) {
		u.logger.Printf("Current version (%s) is up to date", u.config.CurrentVersion)
		return latest, false, nil
	}

	u.logger.Printf("New version available: %s (current: %s)", latest.Version(), u.config.CurrentVersion)
	return latest, true, nil
}

// Update downloads and applies the update
func (u *Updater) Update(ctx context.Context, release *selfupdate.Release) error {
	u.logger.Printf("Downloading update %s...", release.Version())

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("failed to create GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("failed to create updater: %w", err)
	}

	if err := updater.UpdateTo(ctx, release, exe); err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	u.logger.Printf("Successfully updated to version %s", release.Version())
	return nil
}

// CheckAndUpdate checks for updates and applies if available
func (u *Updater) CheckAndUpdate(ctx context.Context) (bool, error) {
	release, needsUpdate, err := u.CheckForUpdate(ctx)
	if err != nil {
		return false, err
	}

	if !needsUpdate {
		return false, nil
	}

	if err := u.Update(ctx, release); err != nil {
		return false, err
	}

	return true, nil
}

// StartPeriodicCheck starts a goroutine that periodically checks for updates
func (u *Updater) StartPeriodicCheck(ctx context.Context, onUpdateAvailable func()) {
	go func() {
		// Wait before first check to allow service to stabilize
		select {
		case <-time.After(StartupDelay):
		case <-ctx.Done():
			return
		}

		ticker := time.NewTicker(u.config.CheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				release, needsUpdate, err := u.CheckForUpdate(ctx)
				if err != nil {
					u.logger.Printf("Update check error: %v", err)
					continue
				}

				if needsUpdate {
					u.logger.Printf("Update available: %s", release.Version())
					if onUpdateAvailable != nil {
						onUpdateAvailable()
					}
				}

			case <-ctx.Done():
				u.logger.Println("Periodic update check stopped")
				return
			}
		}
	}()
}

// GetLatestVersion returns the latest version string without updating
func (u *Updater) GetLatestVersion(ctx context.Context) (string, error) {
	release, found, err := u.CheckForUpdate(ctx)
	if err != nil {
		return "", err
	}
	if !found {
		return u.config.CurrentVersion, nil
	}
	return release.Version(), nil
}
