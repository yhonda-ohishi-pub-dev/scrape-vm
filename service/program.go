package service

import (
	"context"
	"log"
	"net"
	"sync"

	"github.com/kardianos/service"
	pb "github.com/scrape-vm/proto"
	"github.com/scrape-vm/updater"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Program implements service.Interface for Windows service
type Program struct {
	Logger       *log.Logger
	GRPCPort     string
	DownloadPath string
	Headless     bool
	Version      string

	// Auto-update settings
	AutoUpdate     bool
	UpdateInterval string

	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	grpcServer *grpc.Server
	updater    *updater.Updater
}

// Start is called when the service starts
func (p *Program) Start(s service.Service) error {
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Start the main service loop
	go p.run()

	return nil
}

// Stop is called when the service stops
func (p *Program) Stop(s service.Service) error {
	p.Logger.Println("Service stopping...")
	p.cancel()

	// Stop gRPC server gracefully
	if p.grpcServer != nil {
		p.grpcServer.GracefulStop()
	}

	p.wg.Wait()
	p.Logger.Println("Service stopped")
	return nil
}

// run is the main service loop
func (p *Program) run() {
	p.wg.Add(1)
	defer p.wg.Done()

	// Check for updates at startup
	if p.AutoUpdate {
		p.startAutoUpdate()
	}

	// Start gRPC server
	p.runGRPCServer()
}

// startAutoUpdate initializes and starts the auto-updater
func (p *Program) startAutoUpdate() {
	cfg := updater.DefaultConfig(p.Version)
	if p.UpdateInterval != "" {
		if interval, err := updater.ParseDuration(p.UpdateInterval); err == nil {
			cfg.CheckInterval = interval
		}
	}

	p.updater = updater.New(cfg, p.Logger)

	// Check for updates at startup (non-blocking)
	go func() {
		if updated, err := p.updater.CheckAndUpdate(p.ctx); err != nil {
			p.Logger.Printf("Startup update check failed: %v", err)
		} else if updated {
			p.Logger.Println("Update applied, service will restart...")
			if err := updater.RestartService(ServiceName, p.Logger); err != nil {
				p.Logger.Printf("Failed to restart service: %v", err)
			}
		}
	}()

	// Start periodic update checks
	p.updater.StartPeriodicCheck(p.ctx, func() {
		p.Logger.Println("Update available, applying...")
		if _, err := p.updater.CheckAndUpdate(p.ctx); err != nil {
			p.Logger.Printf("Failed to apply update: %v", err)
			return
		}
		p.Logger.Println("Update applied, restarting service...")
		if err := updater.RestartService(ServiceName, p.Logger); err != nil {
			p.Logger.Printf("Failed to restart service: %v", err)
		}
	})
}

// runGRPCServer starts the gRPC server
func (p *Program) runGRPCServer() {
	lis, err := net.Listen("tcp", ":"+p.GRPCPort)
	if err != nil {
		p.Logger.Printf("Failed to listen: %v", err)
		return
	}

	p.grpcServer = grpc.NewServer()
	server := &GRPCServerImpl{
		Logger:       p.Logger,
		DownloadPath: p.DownloadPath,
		Headless:     p.Headless,
		Version:      p.Version,
	}
	pb.RegisterETCScraperServer(p.grpcServer, server)
	reflection.Register(p.grpcServer)

	p.Logger.Printf("gRPC server listening on port %s", p.GRPCPort)
	p.Logger.Printf("Download path: %s", p.DownloadPath)
	p.Logger.Printf("Headless mode: %v", p.Headless)
	p.Logger.Printf("Version: %s", p.Version)

	// Serve until context is cancelled
	go func() {
		<-p.ctx.Done()
		p.grpcServer.GracefulStop()
	}()

	if err := p.grpcServer.Serve(lis); err != nil {
		p.Logger.Printf("gRPC server stopped: %v", err)
	}
}
