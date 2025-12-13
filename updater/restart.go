package updater

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// RestartService restarts the Windows service after update
func RestartService(serviceName string, logger *log.Logger) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("service restart only supported on Windows")
	}

	logger.Println("Scheduling service restart...")

	// Use a goroutine to delay the restart
	go func() {
		// Wait a moment to allow current request to complete
		time.Sleep(2 * time.Second)

		// Stop the service
		stopCmd := exec.Command("sc", "stop", serviceName)
		if err := stopCmd.Run(); err != nil {
			logger.Printf("Warning: failed to stop service: %v", err)
		}

		// Wait for service to stop
		time.Sleep(3 * time.Second)

		// Start the service
		startCmd := exec.Command("sc", "start", serviceName)
		if err := startCmd.Run(); err != nil {
			logger.Printf("Warning: failed to start service: %v", err)
		}
	}()

	return nil
}

// RestartSelf restarts the current process (for non-service mode)
func RestartSelf(logger *log.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	logger.Println("Restarting application...")

	// Start new process with same arguments
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to restart: %w", err)
	}

	// Exit current process
	os.Exit(0)
	return nil
}

// RestartSelfWithArgs restarts the current process with custom arguments
func RestartSelfWithArgs(logger *log.Logger, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	logger.Println("Restarting application with new arguments...")

	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to restart: %w", err)
	}

	os.Exit(0)
	return nil
}
