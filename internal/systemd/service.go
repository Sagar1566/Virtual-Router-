package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ServiceName = "ubuntu-router"
const ServicePath = "/etc/systemd/system/ubuntu-router.service"

// ServiceTemplate is the systemd service file template
// %s is ExecStart command, %s is working directory
const ServiceTemplate = `[Unit]
Description=Ubuntu Router Management
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
WorkingDirectory=%s
Restart=on-failure
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
`

// ServiceStatus represents the current systemd service state
type ServiceStatus struct {
	Installed        bool   `json:"installed"`
	Enabled          bool   `json:"enabled"`
	Running          bool   `json:"running"`
	NeedsUpdate      bool   `json:"needs_update"`
	VerifyOK         bool   `json:"verify_ok"`
	VerifyOutput     string `json:"verify_output,omitempty"`
	CurrentContent   string `json:"current_content,omitempty"`
	ExpectedContent  string `json:"expected_content"`
	BinaryPath       string `json:"binary_path"`
	ConfigPath       string `json:"config_path"`
	WorkingDirectory string `json:"working_directory"`
}

// GenerateServiceContent creates the expected systemd service file content
func GenerateServiceContent(configPath string) string {
	binaryPath := GetBinaryPath()
	workDir := GetWorkingDirectory(configPath)

	// Build ExecStart command
	execStart := binaryPath
	if configPath != "" {
		absConfigPath, err := filepath.Abs(configPath)
		if err == nil {
			configPath = absConfigPath
		}
		execStart = fmt.Sprintf("%s --config %s", binaryPath, configPath)
	}

	return fmt.Sprintf(ServiceTemplate, execStart, workDir)
}

// GetBinaryPath returns the absolute path to the current executable
func GetBinaryPath() string {
	// Default fallback
	binaryPath := "/usr/local/bin/ubuntu-router"

	if execPath, err := os.Executable(); err == nil {
		if absPath, err := filepath.Abs(execPath); err == nil {
			// Resolve symlinks to get the real path
			if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
				binaryPath = realPath
			} else {
				binaryPath = absPath
			}
		}
	}

	return binaryPath
}

// GetWorkingDirectory returns the working directory for the service
func GetWorkingDirectory(configPath string) string {
	// Default working directory
	workDir := "/etc/ubuntu-router"

	if configPath != "" {
		absConfigPath, err := filepath.Abs(configPath)
		if err == nil {
			dir := filepath.Dir(absConfigPath)
			if dir != "" && dir != "." {
				workDir = dir
			}
		}
	}

	return workDir
}

// GetStatus checks the current systemd service state
func GetStatus(configPath string) ServiceStatus {
	status := ServiceStatus{
		BinaryPath:       GetBinaryPath(),
		ConfigPath:       configPath,
		WorkingDirectory: GetWorkingDirectory(configPath),
		ExpectedContent:  GenerateServiceContent(configPath),
	}

	// Check if service file exists
	data, err := os.ReadFile(ServicePath)
	if err != nil {
		// Service not installed
		return status
	}

	status.Installed = true
	status.CurrentContent = string(data)

	// Check if content matches expected
	if strings.TrimSpace(status.CurrentContent) != strings.TrimSpace(status.ExpectedContent) {
		status.NeedsUpdate = true
	}

	// Check if service is enabled
	if err := exec.Command("systemctl", "is-enabled", ServiceName).Run(); err == nil {
		status.Enabled = true
	}

	// Check if service is running
	if err := exec.Command("systemctl", "is-active", ServiceName).Run(); err == nil {
		status.Running = true
	}

	// Verify service file syntax
	output, err := exec.Command("systemd-analyze", "verify", ServicePath).CombinedOutput()
	if err != nil {
		status.VerifyOutput = strings.TrimSpace(string(output))
	} else {
		status.VerifyOK = true
	}

	return status
}

// Install writes the service file and reloads systemd
func Install(configPath string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root to install service")
	}

	content := GenerateServiceContent(configPath)

	if err := os.WriteFile(ServicePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd daemon
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}

// Enable enables the systemd service
func Enable() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root to enable service")
	}

	if err := exec.Command("systemctl", "enable", ServiceName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	return nil
}

// Start starts the systemd service
func Start() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root to start service")
	}

	if err := exec.Command("systemctl", "start", ServiceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

// Restart restarts the systemd service
func Restart() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root to restart service")
	}

	if err := exec.Command("systemctl", "restart", ServiceName).Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	return nil
}
