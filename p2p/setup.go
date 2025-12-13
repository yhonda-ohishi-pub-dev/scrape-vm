package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// SetupConfig configuration for OAuth setup
type SetupConfig struct {
	ServerURL    string        // Base URL of the signaling server (e.g., https://example.com)
	PollInterval time.Duration // Polling interval (default: 2 seconds)
	Timeout      time.Duration // Setup timeout (default: 5 minutes)
}

// SetupResult result from OAuth setup
type SetupResult struct {
	APIKey string
	AppID  string
}

// setupInitResponse response from POST /setup/init
type setupInitResponse struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

// setupPollResponse response from GET /setup/poll
type setupPollResponse struct {
	Status string `json:"status"`
	APIKey string `json:"apiKey,omitempty"`
	AppID  string `json:"appId,omitempty"`
}

// Setup performs OAuth setup flow for Go App using polling method
// It opens a browser for user authentication and polls for completion
func Setup(ctx context.Context, config SetupConfig) (*SetupResult, error) {
	if config.PollInterval == 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}

	// Step 1: Initialize setup session
	initURL := config.ServerURL + "/setup/init"

	req, err := http.NewRequestWithContext(ctx, "POST", initURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create init request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize setup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("setup init failed with status %d: %s", resp.StatusCode, string(body))
	}

	var initResp setupInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, fmt.Errorf("failed to parse init response: %w", err)
	}

	if initResp.Token == "" || initResp.URL == "" {
		return nil, fmt.Errorf("invalid init response: missing token or url")
	}

	// Step 2: Open browser
	fmt.Printf("Opening browser for authentication...\n")
	fmt.Printf("If browser doesn't open automatically, please visit: %s\n", initResp.URL)

	if err := openBrowser(initResp.URL); err != nil {
		fmt.Printf("Warning: failed to open browser automatically: %v\n", err)
		fmt.Printf("Please open the URL manually: %s\n", initResp.URL)
	}

	// Step 3: Poll for completion
	pollURL, err := url.Parse(config.ServerURL + "/setup/poll")
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	q := pollURL.Query()
	q.Set("token", initResp.Token)
	pollURL.RawQuery = q.Encode()

	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			if ctx.Err() != nil {
				return nil, fmt.Errorf("setup cancelled")
			}
			return nil, fmt.Errorf("setup timed out after %v", config.Timeout)

		case <-ticker.C:
			// Poll for status
			pollReq, err := http.NewRequestWithContext(timeoutCtx, "GET", pollURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create poll request: %w", err)
			}

			pollResp, err := http.DefaultClient.Do(pollReq)
			if err != nil {
				return nil, fmt.Errorf("failed to poll setup status: %w", err)
			}

			if pollResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(pollResp.Body)
				pollResp.Body.Close()
				return nil, fmt.Errorf("poll failed with status %d: %s", pollResp.StatusCode, string(body))
			}

			var pollResult setupPollResponse
			if err := json.NewDecoder(pollResp.Body).Decode(&pollResult); err != nil {
				pollResp.Body.Close()
				return nil, fmt.Errorf("failed to parse poll response: %w", err)
			}
			pollResp.Body.Close()

			switch pollResult.Status {
			case "complete":
				if pollResult.APIKey == "" || pollResult.AppID == "" {
					return nil, fmt.Errorf("invalid poll response: missing apiKey or appId")
				}
				fmt.Printf("Setup completed successfully!\n")
				return &SetupResult{
					APIKey: pollResult.APIKey,
					AppID:  pollResult.AppID,
				}, nil

			case "pending":
				// Continue polling
				continue

			default:
				return nil, fmt.Errorf("unknown status from poll: %s", pollResult.Status)
			}
		}
	}
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// SaveCredentials saves API key to a file (helper function)
func SaveCredentials(path string, result *SetupResult) error {
	data := fmt.Sprintf("API_KEY=%s\nAPP_ID=%s\n", result.APIKey, result.AppID)
	return os.WriteFile(path, []byte(data), 0600)
}

// LoadCredentials loads API key from a file (helper function)
func LoadCredentials(path string) (*SetupResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := &SetupResult{}
	lines := splitLines(string(data))

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		key, value := parseKeyValue(line)
		switch key {
		case "API_KEY", "P2P_API_KEY":
			result.APIKey = value
		case "APP_ID", "P2P_APP_ID":
			result.AppID = value
		}
	}

	if result.APIKey == "" {
		return nil, fmt.Errorf("API_KEY not found in credentials file")
	}

	return result, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseKeyValue(line string) (string, string) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' {
			return line[:i], line[i+1:]
		}
	}
	return line, ""
}
