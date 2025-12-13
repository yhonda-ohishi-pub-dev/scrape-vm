package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	svc "github.com/kardianos/service"
)

// Manager handles service management operations
type Manager struct {
	service svc.Service
	logger  svc.Logger
	program *Program
}

// NewManager creates a new service manager
func NewManager(prg *Program) (*Manager, error) {
	// Get executable path for service registration
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build service arguments
	args := buildServiceArgs(prg)

	cfg := NewServiceConfig(exePath, args)

	s, err := svc.New(prg, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	logger, err := s.Logger(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get service logger: %w", err)
	}

	return &Manager{
		service: s,
		logger:  logger,
		program: prg,
	}, nil
}

// buildServiceArgs builds the command line arguments for the service
func buildServiceArgs(prg *Program) []string {
	args := []string{"-grpc", "-port=" + prg.GRPCPort}

	// Use absolute path for download directory
	downloadPath := prg.DownloadPath
	if !filepath.IsAbs(downloadPath) {
		if absPath, err := filepath.Abs(downloadPath); err == nil {
			downloadPath = absPath
		}
	}
	args = append(args, "-download="+downloadPath)

	if prg.Headless {
		args = append(args, "-headless=true")
	} else {
		args = append(args, "-headless=false")
	}

	if prg.AutoUpdate {
		args = append(args, "-auto-update=true")
	} else {
		args = append(args, "-auto-update=false")
	}

	if prg.UpdateInterval != "" {
		args = append(args, "-update-interval="+prg.UpdateInterval)
	}

	return args
}

// Install installs the service
func (m *Manager) Install() error {
	return m.service.Install()
}

// Uninstall uninstalls the service
func (m *Manager) Uninstall() error {
	return m.service.Uninstall()
}

// Start starts the service
func (m *Manager) Start() error {
	return m.service.Start()
}

// Stop stops the service
func (m *Manager) Stop() error {
	return m.service.Stop()
}

// Run runs the service (called by SCM)
func (m *Manager) Run() error {
	return m.service.Run()
}

// Status returns the service status
func (m *Manager) Status() (svc.Status, error) {
	return m.service.Status()
}

// RunServiceCommand handles service management commands
func RunServiceCommand(cmd string, prg *Program, logger *log.Logger) error {
	mgr, err := NewManager(prg)
	if err != nil {
		return err
	}

	switch cmd {
	case "install":
		if err := mgr.Install(); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}
		logger.Println("Service installed successfully")
		logger.Printf("Service name: %s", ServiceName)
		logger.Println("To start the service, run: etc-scraper.exe -service start")

	case "uninstall":
		// Try to stop first
		_ = mgr.Stop()

		if err := mgr.Uninstall(); err != nil {
			return fmt.Errorf("failed to uninstall service: %w", err)
		}
		logger.Println("Service uninstalled successfully")

	case "start":
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
		logger.Println("Service started successfully")

	case "stop":
		if err := mgr.Stop(); err != nil {
			return fmt.Errorf("failed to stop service: %w", err)
		}
		logger.Println("Service stopped successfully")

	case "restart":
		_ = mgr.Stop()
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to restart service: %w", err)
		}
		logger.Println("Service restarted successfully")

	case "status":
		status, err := mgr.Status()
		if err != nil {
			return fmt.Errorf("failed to get service status: %w", err)
		}
		printStatus(status, logger)

	case "run":
		// Run as service (called by SCM)
		return mgr.Run()

	default:
		return fmt.Errorf("unknown service command: %s\nValid commands: install, uninstall, start, stop, restart, status", cmd)
	}

	return nil
}

func printStatus(status svc.Status, logger *log.Logger) {
	switch status {
	case svc.StatusRunning:
		logger.Println("Service status: Running")
	case svc.StatusStopped:
		logger.Println("Service status: Stopped")
	default:
		logger.Println("Service status: Unknown")
	}
}
