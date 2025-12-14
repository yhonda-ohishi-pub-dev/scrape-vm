package p2p

import (
	"context"

	"github.com/anthropics/cf-wbrtc-auth/go/client"
)

// SetupConfig configuration for OAuth setup (wraps client.SetupConfig)
type SetupConfig = client.SetupConfig

// SetupResult result from OAuth setup (wraps client.SetupResult)
type SetupResult = client.SetupResult

// RefreshConfig configuration for token refresh
type RefreshConfig = client.RefreshAPIKeyConfig

// Setup performs OAuth setup flow for Go App using polling method
// Delegates to client.Setup
func Setup(ctx context.Context, config SetupConfig) (*SetupResult, error) {
	return client.Setup(ctx, config)
}

// SaveCredentials saves API key to a file
// Delegates to client.SaveCredentials
func SaveCredentials(path string, result *SetupResult) error {
	return client.SaveCredentials(path, result)
}

// LoadCredentials loads API key from a file
// Delegates to client.LoadCredentials
func LoadCredentials(path string) (*SetupResult, error) {
	return client.LoadCredentials(path)
}

// RefreshAPIKey refreshes the API key using refresh token
// Delegates to client.RefreshAPIKey
func RefreshAPIKey(ctx context.Context, config RefreshConfig) (*SetupResult, error) {
	result, err := client.RefreshAPIKey(ctx, config)
	if err != nil {
		return nil, err
	}
	return &SetupResult{
		APIKey:       result.APIKey,
		AppID:        result.AppID,
		RefreshToken: result.RefreshToken,
	}, nil
}
