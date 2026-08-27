package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/logger"
	"github.com/iodesystems/ubuntu-router/internal/server"
	"github.com/iodesystems/ubuntu-router/internal/systemd"
)

var version = "dev"

func main() {
	var (
		configPath     = flag.String("config", "", "Path to config file (default: auto-discover)")
		listenAddr     = flag.String("listen", ":8080", "HTTP listen address")
		dryRun         = flag.Bool("dry-run", false, "Dry run mode (no system changes)")
		showVersion    = flag.Bool("version", false, "Show version")
		install        = flag.Bool("install", false, "Install systemd service")
		check          = flag.Bool("check", false, "Check system requirements")
		configTemplate = flag.Bool("config-template", false, "Print example config")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("ubuntu-router %s\n", version)
		return
	}

	if *configTemplate {
		printConfigTemplate()
		return
	}

	if *install {
		if err := installSystemdService(*configPath); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
		return
	}

	// Check if running as root
	if os.Geteuid() != 0 && !*dryRun {
		log.Fatal("ubuntu-router must be run as root (use sudo)")
	}

	// Load or create config
	cfg, cfgPath, err := config.LoadOrCreate(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override listen address if specified via CLI flag
	// This takes precedence over config file settings
	if *listenAddr != ":8080" {
		// CLI flag specified - use it as the sole listen address
		cfg.WebListenAddresses = []string{*listenAddr}
		cfg.ListenAddr = *listenAddr
	}

	if *check {
		if err := runSystemCheck(cfg); err != nil {
			log.Fatalf("System check failed: %v", err)
		}
		return
	}

	// Initialize logger
	logPath := "/var/log/ubuntu-router.log"
	if *dryRun {
		logPath = "./ubuntu-router.log"
	}
	if err := logger.Init(logPath); err != nil {
		log.Printf("Warning: failed to initialize file logger: %v", err)
	}
	logger.Info("ubuntu-router %s starting", version)

	// Create and run server
	srv, err := server.New(cfg, cfgPath, *dryRun, version)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("ubuntu-router %s starting on %s", version, cfg.ListenAddr)
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func printConfigTemplate() {
	fmt.Print(config.ExampleConfig)
}

func installSystemdService(configPath string) error {
	if err := systemd.Install(configPath); err != nil {
		return err
	}

	fmt.Printf("Installed %s\n", systemd.ServicePath)
	fmt.Printf("Binary: %s\n", systemd.GetBinaryPath())
	if configPath != "" {
		fmt.Printf("Config: %s\n", configPath)
	}
	fmt.Println()
	fmt.Println("Run: systemctl daemon-reload && systemctl enable --now ubuntu-router")
	return nil
}

func runSystemCheck(cfg *config.Config) error {
	fmt.Println("=== Ubuntu Router System Check ===")
	fmt.Println()

	checks := []struct {
		name  string
		check func() (bool, string)
	}{
		{"Root privileges", checkRoot},
		{"dnsmasq installed", checkDnsmasq},
		{"WireGuard installed", checkWireguard},
		{"wg-quick installed", checkWgQuick},
		{"WireGuard kernel module", checkWireguardModule},
		{"WireGuard config directory", checkWireguardConfigDir},
		{"hostapd installed", checkHostapd},
		{"iptables installed", checkIptables},
		{"Network interfaces", checkInterfaces},
	}

	allPassed := true
	for _, c := range checks {
		ok, msg := c.check()
		status := "✓"
		if !ok {
			status = "✗"
			allPassed = false
		}
		fmt.Printf("  %s %s: %s\n", status, c.name, msg)
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All checks passed!")
		return nil
	}
	return fmt.Errorf("some checks failed")
}

func checkRoot() (bool, string) {
	if os.Geteuid() == 0 {
		return true, "running as root"
	}
	return false, "not running as root"
}

func checkDnsmasq() (bool, string) {
	if _, err := os.Stat("/usr/sbin/dnsmasq"); err == nil {
		return true, "found at /usr/sbin/dnsmasq"
	}
	return false, "not found (apt install dnsmasq)"
}

func checkWireguard() (bool, string) {
	if _, err := os.Stat("/usr/bin/wg"); err != nil {
		return false, "wg not found (apt install wireguard)"
	}
	return true, "found at /usr/bin/wg"
}

func checkWgQuick() (bool, string) {
	if _, err := os.Stat("/usr/bin/wg-quick"); err != nil {
		return false, "wg-quick not found (apt install wireguard-tools)"
	}
	return true, "found at /usr/bin/wg-quick"
}

func checkWireguardModule() (bool, string) {
	// Check if module is loaded
	if _, err := os.Stat("/sys/module/wireguard"); err == nil {
		return true, "kernel module loaded"
	}
	// Check if module is available (built-in or loadable)
	// Try to check /lib/modules for wireguard.ko
	entries, err := os.ReadDir("/lib/modules")
	if err == nil && len(entries) > 0 {
		// Check if wireguard is built into the kernel (Linux 5.6+)
		data, err := os.ReadFile("/proc/version")
		if err == nil {
			version := string(data)
			// WireGuard is built into Linux 5.6+
			if strings.Contains(version, "Linux version 5.") ||
				strings.Contains(version, "Linux version 6.") {
				return true, "kernel 5.6+ (WireGuard built-in, may need modprobe)"
			}
		}
	}
	return false, "module not loaded (try: modprobe wireguard)"
}

func checkWireguardConfigDir() (bool, string) {
	info, err := os.Stat("/etc/wireguard")
	if os.IsNotExist(err) {
		return false, "directory /etc/wireguard does not exist (mkdir -p /etc/wireguard && chmod 700 /etc/wireguard)"
	}
	if err != nil {
		return false, fmt.Sprintf("failed to check /etc/wireguard: %v", err)
	}
	if !info.IsDir() {
		return false, "/etc/wireguard exists but is not a directory"
	}
	// Check permissions (should be 0700)
	perm := info.Mode().Perm()
	if perm != 0700 {
		return true, fmt.Sprintf("directory exists but permissions are %04o (should be 0700)", perm)
	}
	return true, "directory exists with correct permissions (0700)"
}

func checkHostapd() (bool, string) {
	if _, err := os.Stat("/usr/sbin/hostapd"); err == nil {
		return true, "found at /usr/sbin/hostapd"
	}
	return false, "not found (apt install hostapd)"
}

func checkIptables() (bool, string) {
	if _, err := os.Stat("/usr/sbin/iptables"); err == nil {
		return true, "found at /usr/sbin/iptables"
	}
	return false, "not found (apt install iptables)"
}

func checkInterfaces() (bool, string) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return false, fmt.Sprintf("failed to read /sys/class/net: %v", err)
	}
	var ifaces []string
	for _, e := range entries {
		if e.Name() != "lo" {
			ifaces = append(ifaces, e.Name())
		}
	}
	if len(ifaces) == 0 {
		return false, "no network interfaces found"
	}
	return true, fmt.Sprintf("found %d interfaces", len(ifaces))
}
