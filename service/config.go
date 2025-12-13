package service

import "github.com/kardianos/service"

const (
	ServiceName        = "etc-scraper"
	ServiceDisplayName = "ETC Scraper Service"
	ServiceDescription = "ETC Meisai Scraper gRPC Server - Automatically downloads ETC usage statements"
)

// NewServiceConfig creates a new service configuration
func NewServiceConfig(exePath string, args []string) *service.Config {
	cfg := &service.Config{
		Name:        ServiceName,
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		Arguments:   args,
	}

	// Windows-specific options
	cfg.Option = service.KeyValue{
		"StartType": "automatic",
	}

	return cfg
}
