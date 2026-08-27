package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// FileSystem abstracts file operations for testing
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
	Symlink(oldname, newname string) error
	ReadLink(path string) (string, error)
}

// CommandRunner abstracts command execution for testing
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunWithStdin(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error)
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

// Process represents a running process
type Process interface {
	Wait() error
	Kill() error
	Pid() int
}

// RealFileSystem implements FileSystem using the real filesystem
type RealFileSystem struct{}

func (RealFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (RealFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (RealFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (RealFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (RealFileSystem) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (RealFileSystem) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (RealFileSystem) ReadLink(path string) (string, error) {
	return os.Readlink(path)
}

// RealCommandRunner implements CommandRunner using real exec
type RealCommandRunner struct{}

func (RealCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (RealCommandRunner) RunWithStdin(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	return cmd.CombinedOutput()
}

func (RealCommandRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &realProcess{cmd: cmd}, nil
}

type realProcess struct {
	cmd *exec.Cmd
}

func (p *realProcess) Wait() error { return p.cmd.Wait() }
func (p *realProcess) Kill() error { return p.cmd.Process.Kill() }
func (p *realProcess) Pid() int    { return p.cmd.Process.Pid }

// DryRunFileSystem implements FileSystem for dry-run mode
type DryRunFileSystem struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]bool
	log   []string
}

func NewDryRunFileSystem() *DryRunFileSystem {
	return &DryRunFileSystem{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (fs *DryRunFileSystem) ReadFile(path string) ([]byte, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if data, ok := fs.files[path]; ok {
		return data, nil
	}
	// Fall back to real filesystem for reading
	return os.ReadFile(path)
}

func (fs *DryRunFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.files[path] = data
	fs.log = append(fs.log, fmt.Sprintf("WriteFile(%s, %d bytes, %o)", path, len(data), perm))
	return nil
}

func (fs *DryRunFileSystem) MkdirAll(path string, perm os.FileMode) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.dirs[path] = true
	fs.log = append(fs.log, fmt.Sprintf("MkdirAll(%s, %o)", path, perm))
	return nil
}

func (fs *DryRunFileSystem) Remove(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	delete(fs.files, path)
	fs.log = append(fs.log, fmt.Sprintf("Remove(%s)", path))
	return nil
}

func (fs *DryRunFileSystem) Stat(path string) (os.FileInfo, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if _, ok := fs.files[path]; ok {
		return &dryRunFileInfo{name: path, size: int64(len(fs.files[path]))}, nil
	}
	if fs.dirs[path] {
		return &dryRunFileInfo{name: path, isDir: true}, nil
	}
	return os.Stat(path)
}

func (fs *DryRunFileSystem) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (fs *DryRunFileSystem) Symlink(oldname, newname string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.log = append(fs.log, fmt.Sprintf("Symlink(%s -> %s)", newname, oldname))
	return nil
}

func (fs *DryRunFileSystem) ReadLink(path string) (string, error) {
	return os.Readlink(path)
}

func (fs *DryRunFileSystem) Log() []string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return append([]string{}, fs.log...)
}

type dryRunFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *dryRunFileInfo) Name() string       { return fi.name }
func (fi *dryRunFileInfo) Size() int64        { return fi.size }
func (fi *dryRunFileInfo) Mode() os.FileMode  { return 0644 }
func (fi *dryRunFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *dryRunFileInfo) IsDir() bool        { return fi.isDir }
func (fi *dryRunFileInfo) Sys() any           { return nil }

// DryRunCommandRunner implements CommandRunner for dry-run mode
type DryRunCommandRunner struct {
	mu  sync.Mutex
	log []string
}

func NewDryRunCommandRunner() *DryRunCommandRunner {
	return &DryRunCommandRunner{}
}

func (r *DryRunCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cmdStr := fmt.Sprintf("%s %v", name, args)
	r.log = append(r.log, cmdStr)

	// For read-only inspection commands, run them safely
	switch name {
	case "ip":
		isReadOnly := false
		for _, arg := range args {
			if arg == "show" || arg == "list" {
				isReadOnly = true
			}
			if arg == "set" || arg == "add" || arg == "del" || arg == "delete" || arg == "flush" {
				isReadOnly = false
				break
			}
		}
		if isReadOnly {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		}
	case "cat":
		cmd := exec.CommandContext(ctx, name, args...)
		return cmd.CombinedOutput()
	case "wg":
		if len(args) > 0 && args[0] == "show" {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		}
	}

	return []byte(fmt.Sprintf("[dry-run] %s", cmdStr)), nil
}

func (r *DryRunCommandRunner) RunWithStdin(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var buf bytes.Buffer
	io.Copy(&buf, stdin)
	cmdStr := fmt.Sprintf("%s %v (stdin: %d bytes)", name, args, buf.Len())
	r.log = append(r.log, cmdStr)

	return []byte(fmt.Sprintf("[dry-run] %s", cmdStr)), nil
}

func (r *DryRunCommandRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cmdStr := fmt.Sprintf("%s %v (background)", name, args)
	r.log = append(r.log, cmdStr)

	return &dryRunProcess{}, nil
}

func (r *DryRunCommandRunner) Log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.log...)
}

type dryRunProcess struct{}

func (p *dryRunProcess) Wait() error { return nil }
func (p *dryRunProcess) Kill() error { return nil }
func (p *dryRunProcess) Pid() int    { return 0 }

// ServiceManager handles systemd service operations
type ServiceManager struct {
	runner CommandRunner
}

func NewServiceManager(runner CommandRunner) *ServiceManager {
	return &ServiceManager{runner: runner}
}

func (sm *ServiceManager) Start(ctx context.Context, service string) error {
	out, err := sm.runner.Run(ctx, "systemctl", "start", service)
	if err != nil {
		// Get more details from journalctl
		journalOut, _ := sm.runner.Run(ctx, "journalctl", "-u", service, "-n", "20", "--no-pager")
		return fmt.Errorf("failed to start %s: %v\nOutput: %s\nJournal: %s", service, err, string(out), string(journalOut))
	}
	return nil
}

func (sm *ServiceManager) Stop(ctx context.Context, service string) error {
	out, err := sm.runner.Run(ctx, "systemctl", "stop", service)
	if err != nil {
		return fmt.Errorf("failed to stop %s: %v\nOutput: %s", service, err, string(out))
	}
	return nil
}

func (sm *ServiceManager) Restart(ctx context.Context, service string) error {
	out, err := sm.runner.Run(ctx, "systemctl", "restart", service)
	if err != nil {
		journalOut, _ := sm.runner.Run(ctx, "journalctl", "-u", service, "-n", "20", "--no-pager")
		return fmt.Errorf("failed to restart %s: %v\nOutput: %s\nJournal: %s", service, err, string(out), string(journalOut))
	}
	return nil
}

func (sm *ServiceManager) Reload(ctx context.Context, service string) error {
	_, err := sm.runner.Run(ctx, "systemctl", "reload", service)
	return err
}

func (sm *ServiceManager) Enable(ctx context.Context, service string) error {
	_, err := sm.runner.Run(ctx, "systemctl", "enable", service)
	return err
}

func (sm *ServiceManager) Disable(ctx context.Context, service string) error {
	_, err := sm.runner.Run(ctx, "systemctl", "disable", service)
	return err
}

func (sm *ServiceManager) Status(ctx context.Context, service string) (string, error) {
	out, err := sm.runner.Run(ctx, "systemctl", "is-active", service)
	return string(bytes.TrimSpace(out)), err
}

func (sm *ServiceManager) IsActive(ctx context.Context, service string) bool {
	status, _ := sm.Status(ctx, service)
	return status == "active"
}

func (sm *ServiceManager) IsEnabled(ctx context.Context, service string) bool {
	out, err := sm.runner.Run(ctx, "systemctl", "is-enabled", service)
	if err != nil {
		return false
	}
	status := string(bytes.TrimSpace(out))
	return status == "enabled"
}

// Dependency represents a system dependency
type Dependency struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	InstallCmd  string `json:"install_cmd,omitempty"`
}

// DependencyChecker checks for required system dependencies
type DependencyChecker struct {
	runner CommandRunner
}

// NewDependencyChecker creates a new dependency checker
func NewDependencyChecker(runner CommandRunner) *DependencyChecker {
	return &DependencyChecker{runner: runner}
}

// CheckAll checks all dependencies and returns their status
func (dc *DependencyChecker) CheckAll(ctx context.Context) []Dependency {
	deps := []Dependency{
		{
			Name:        "iw",
			Description: "Wireless configuration tool",
			Required:    true,
			InstallCmd:  "apt install iw",
		},
		{
			Name:        "hostapd",
			Description: "WiFi access point daemon",
			Required:    true,
			InstallCmd:  "apt install hostapd",
		},
		{
			Name:        "dnsmasq",
			Description: "DNS and DHCP server",
			Required:    true,
			InstallCmd:  "apt install dnsmasq",
		},
		{
			Name:        "iptables",
			Description: "Firewall and NAT",
			Required:    true,
			InstallCmd:  "apt install iptables",
		},
		{
			Name:        "wg",
			Description: "WireGuard VPN",
			Required:    false,
			InstallCmd:  "apt install wireguard-tools",
		},
		{
			Name:        "netplan",
			Description: "Network configuration",
			Required:    false,
			InstallCmd:  "apt install netplan.io",
		},
		{
			Name:        "ip",
			Description: "Network interface management (iproute2)",
			Required:    true,
			InstallCmd:  "apt install iproute2",
		},
		{
			Name:        "haproxy",
			Description: "HAProxy load balancer (for Services)",
			Required:    false,
			InstallCmd:  "apt install haproxy",
		},
	}

	for i := range deps {
		deps[i].Installed, deps[i].Version = dc.checkCommand(ctx, deps[i].Name)
	}

	return deps
}

// checkCommand checks if a command is available and gets its version
func (dc *DependencyChecker) checkCommand(ctx context.Context, name string) (bool, string) {
	// First check if command exists
	out, err := dc.runner.Run(ctx, "which", name)
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return false, ""
	}

	// Try to get version
	version := ""
	switch name {
	case "iw":
		if out, err := dc.runner.Run(ctx, "iw", "--version"); err == nil {
			version = extractVersion(string(out))
		}
	case "hostapd":
		if out, err := dc.runner.Run(ctx, "hostapd", "-v"); err == nil {
			version = extractVersion(string(out))
		}
	case "dnsmasq":
		if out, err := dc.runner.Run(ctx, "dnsmasq", "--version"); err == nil {
			version = extractVersion(string(out))
		}
	case "iptables":
		if out, err := dc.runner.Run(ctx, "iptables", "--version"); err == nil {
			version = extractVersion(string(out))
		}
	case "wg":
		if out, err := dc.runner.Run(ctx, "wg", "--version"); err == nil {
			version = extractVersion(string(out))
		}
	case "ip":
		if out, err := dc.runner.Run(ctx, "ip", "-V"); err == nil {
			version = extractVersion(string(out))
		}
	case "netplan":
		if out, err := dc.runner.Run(ctx, "netplan", "--version"); err == nil {
			version = extractVersion(string(out))
		}
	case "haproxy":
		if out, err := dc.runner.Run(ctx, "haproxy", "-v"); err == nil {
			version = extractVersion(string(out))
		}
	}

	return true, version
}

// extractVersion extracts version string from command output
func extractVersion(output string) string {
	// Take first line, trim whitespace
	lines := bytes.Split([]byte(output), []byte("\n"))
	if len(lines) > 0 {
		return string(bytes.TrimSpace(lines[0]))
	}
	return ""
}

// AllRequiredInstalled returns true if all required dependencies are installed
func (dc *DependencyChecker) AllRequiredInstalled(ctx context.Context) bool {
	deps := dc.CheckAll(ctx)
	for _, dep := range deps {
		if dep.Required && !dep.Installed {
			return false
		}
	}
	return true
}

// GetMissingRequired returns list of missing required dependencies
func (dc *DependencyChecker) GetMissingRequired(ctx context.Context) []Dependency {
	deps := dc.CheckAll(ctx)
	var missing []Dependency
	for _, dep := range deps {
		if dep.Required && !dep.Installed {
			missing = append(missing, dep)
		}
	}
	return missing
}
