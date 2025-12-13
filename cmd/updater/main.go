package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/kardianos/service"
	"github.com/scrape-vm/updater"
)

// Version is set at build time
var Version = "dev"

const (
	ServiceName        = "etc-scraper-updater"
	ServiceDisplayName = "ETC Scraper Auto-Updater"
	ServiceDescription = "Monitors and updates the ETC Scraper service automatically"
	TargetServiceName  = "etc-scraper"
	TargetBinaryName   = "etc-scraper.exe"
)

// Program implements service.Interface
type Program struct {
	logger         *log.Logger
	logFile        *os.File
	config         *Config
	ctx            context.Context
	cancel         context.CancelFunc
	updaterService *updater.Updater
}

// Config holds the updater configuration
type Config struct {
	TargetServiceName string
	TargetBinaryPath  string
	CheckInterval     time.Duration
	StartupDelay      time.Duration
}

func main() {
	// Flags
	serviceCmd := flag.String("service", "", "Service command: install|uninstall|start|stop|status|run")
	targetBinary := flag.String("target", "", "Path to target binary (default: same directory as updater)")
	checkInterval := flag.String("interval", "1h", "Update check interval (e.g., 1h, 30m)")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("etc-scraper-updater version %s\n", Version)
		return
	}

	logger := log.New(os.Stdout, "[UPDATER] ", log.LstdFlags)

	// Parse check interval
	interval, err := time.ParseDuration(*checkInterval)
	if err != nil {
		interval = 1 * time.Hour
	}

	// Determine target binary path
	targetPath := *targetBinary
	if targetPath == "" {
		exePath, _ := os.Executable()
		targetPath = filepath.Join(filepath.Dir(exePath), TargetBinaryName)
	}

	config := &Config{
		TargetServiceName: TargetServiceName,
		TargetBinaryPath:  targetPath,
		CheckInterval:     interval,
		StartupDelay:      30 * time.Second,
	}

	prg := &Program{
		logger: logger,
		config: config,
	}

	svcConfig := &service.Config{
		Name:        ServiceName,
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		Arguments:   buildServiceArgs(config),
		Option: service.KeyValue{
			"StartType": "automatic",
		},
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		logger.Fatalf("Failed to create service: %v", err)
	}

	// Handle service commands
	if *serviceCmd != "" {
		switch *serviceCmd {
		case "install":
			if err := s.Install(); err != nil {
				logger.Fatalf("Failed to install service: %v", err)
			}
			logger.Printf("Service installed: %s", ServiceName)
			logger.Printf("Target binary: %s", targetPath)
			logger.Printf("Check interval: %s", interval)
			logger.Println("Run 'etc-scraper-updater -service start' to start the service")

		case "uninstall":
			_ = s.Stop()
			if err := s.Uninstall(); err != nil {
				logger.Fatalf("Failed to uninstall service: %v", err)
			}
			logger.Println("Service uninstalled")

		case "start":
			if err := s.Start(); err != nil {
				logger.Fatalf("Failed to start service: %v", err)
			}
			logger.Println("Service started")

		case "stop":
			if err := s.Stop(); err != nil {
				logger.Fatalf("Failed to stop service: %v", err)
			}
			logger.Println("Service stopped")

		case "status":
			status, err := s.Status()
			if err != nil {
				logger.Fatalf("Failed to get status: %v", err)
			}
			switch status {
			case service.StatusRunning:
				logger.Println("Service status: Running")
			case service.StatusStopped:
				logger.Println("Service status: Stopped")
			default:
				logger.Println("Service status: Unknown")
			}

		case "run":
			// Run as service (called by SCM)
			if err := s.Run(); err != nil {
				logger.Fatalf("Service run failed: %v", err)
			}

		default:
			logger.Fatalf("Unknown command: %s\nValid commands: install, uninstall, start, stop, status, run", *serviceCmd)
		}
		return
	}

	// Run interactively (for testing)
	logger.Println("Running interactively. Press Ctrl+C to stop.")
	if err := s.Run(); err != nil {
		logger.Fatalf("Failed to run: %v", err)
	}
}

func buildServiceArgs(config *Config) []string {
	args := []string{"-service", "run"}

	if config.TargetBinaryPath != "" {
		args = append(args, "-target="+config.TargetBinaryPath)
	}

	if config.CheckInterval != 0 {
		args = append(args, fmt.Sprintf("-interval=%s", config.CheckInterval))
	}

	return args
}

// Start is called when the service starts
func (p *Program) Start(s service.Service) error {
	svcLogger, _ := s.Logger(nil)
	if svcLogger != nil {
		svcLogger.Info("Updater service starting...")
	}

	if err := p.setupFileLogger(); err != nil {
		if svcLogger != nil {
			svcLogger.Error("Failed to setup file logger: " + err.Error())
		}
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.run()
	return nil
}

// Stop is called when the service stops
func (p *Program) Stop(s service.Service) error {
	if p.logger != nil {
		p.logger.Println("Updater service stopping...")
	}
	if p.cancel != nil {
		p.cancel()
	}

	if p.logFile != nil {
		p.logFile.Close()
	}
	return nil
}

// setupFileLogger sets up file logging
func (p *Program) setupFileLogger() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log dir %s: %w", logDir, err)
	}

	logFile := filepath.Join(logDir, "etc-scraper-updater.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	p.logFile = f
	mw := io.MultiWriter(os.Stdout, f)
	p.logger = log.New(mw, "[UPDATER] ", log.LstdFlags)
	return nil
}

// run is the main updater loop
func (p *Program) run() {
	// Ensure logger is available
	if p.logger == nil {
		p.logger = log.New(os.Stderr, "[UPDATER] ", log.LstdFlags)
	}

	// Recover from panic
	defer func() {
		if r := recover(); r != nil {
			p.logger.Printf("run() panic recovered: %v", r)
		}
	}()

	p.logger.Printf("Starting updater service...")
	p.logger.Printf("Target service: %s", p.config.TargetServiceName)
	p.logger.Printf("Target binary: %s", p.config.TargetBinaryPath)
	p.logger.Printf("Check interval: %s", p.config.CheckInterval)
	p.logger.Printf("Version: %s", Version)

	// Create updater instance
	cfg := updater.DefaultConfig(Version)
	cfg.CheckInterval = p.config.CheckInterval
	p.updaterService = updater.New(cfg, p.logger)

	// Wait for startup delay
	p.logger.Printf("Waiting %s before first update check...", p.config.StartupDelay)
	select {
	case <-time.After(p.config.StartupDelay):
	case <-p.ctx.Done():
		p.logger.Println("Updater service stopped during startup delay")
		return
	}

	// Initial check
	p.checkAndApplyUpdate()

	// Periodic check loop
	ticker := time.NewTicker(p.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkAndApplyUpdate()
		case <-p.ctx.Done():
			p.logger.Println("Updater service stopped")
			return
		}
	}
}

// checkAndApplyUpdate checks for updates and applies if available
func (p *Program) checkAndApplyUpdate() {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Printf("Update check panic: %v", r)
		}
	}()

	p.logger.Println("Checking for updates...")

	// Check for update
	release, needsUpdate, err := p.updaterService.CheckForUpdate(p.ctx)
	if err != nil {
		p.logger.Printf("Update check failed: %v", err)
		return
	}

	if !needsUpdate {
		p.logger.Println("No update available")
		return
	}

	p.logger.Printf("Update available: %s", release.Version())

	// Stop target service before updating
	p.logger.Printf("Stopping target service: %s", p.config.TargetServiceName)
	if err := p.stopTargetService(); err != nil {
		p.logger.Printf("Warning: Failed to stop target service: %v", err)
		// Continue anyway - service might not be running
	}

	// Wait for service to fully stop
	time.Sleep(3 * time.Second)

	// Download and apply update to target binary
	p.logger.Printf("Downloading update %s...", release.Version())
	if err := p.applyUpdateToTarget(release); err != nil {
		p.logger.Printf("Update failed: %v", err)
		// Try to restart service even if update failed
		p.startTargetService()
		return
	}

	p.logger.Printf("Update applied successfully to version %s", release.Version())

	// Start target service
	p.logger.Printf("Starting target service: %s", p.config.TargetServiceName)
	if err := p.startTargetService(); err != nil {
		p.logger.Printf("Failed to start target service: %v", err)
	} else {
		p.logger.Println("Target service started successfully")
	}
}

// applyUpdateToTarget downloads and applies update to the target binary
func (p *Program) applyUpdateToTarget(release *selfupdate.Release) error {
	// Use UpdateTo to update the target binary (not this executable)
	return p.updaterService.UpdateTo(p.ctx, release, p.config.TargetBinaryPath)
}

// stopTargetService stops the target Windows service
func (p *Program) stopTargetService() error {
	cmd := exec.Command("sc", "stop", p.config.TargetServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc stop failed: %v, output: %s", err, string(output))
	}
	return nil
}

// startTargetService starts the target Windows service
func (p *Program) startTargetService() error {
	cmd := exec.Command("sc", "start", p.config.TargetServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc start failed: %v, output: %s", err, string(output))
	}
	return nil
}
