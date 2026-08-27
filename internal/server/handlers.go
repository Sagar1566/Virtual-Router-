package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iodesystems/ubuntu-router/internal/config"
)

// Admin dashboard
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Gather status information
	interfaces, _ := s.network.ListInterfaces(ctx)
	defaultIface, _ := s.network.GetDefaultInterface(ctx)
	ipForwardEnabled := s.network.IsIPForwardEnabled()

	var dnsStatus interface{}
	var leases interface{}
	if s.config.DNSEnabled {
		dnsStatus, _ = s.dns.GetStatus(ctx, s.config)
		leases, _ = s.dns.GetLeases()
	}

	var wgStatus interface{}
	if s.config.WireGuard != nil && s.config.WireGuard.Enabled {
		wgStatus, _ = s.wireguard.GetFullStatus(ctx, s.config.WireGuard)
	}

	var wifiStatus interface{}
	if len(s.config.WiFiInterfaces) > 0 {
		wifiStatus, _ = s.wifi.GetStatus(ctx, s.config.WiFiInterfaces)
	}

	data := map[string]interface{}{
		"Config":           s.config,
		"Interfaces":       interfaces,
		"DefaultInterface": defaultIface,
		"IPForwardEnabled": ipForwardEnabled,
		"DNSStatus":        dnsStatus,
		"Leases":           leases,
		"WireGuardStatus":  wgStatus,
		"WiFiStatus":       wifiStatus,
		"Message":          r.URL.Query().Get("msg"),
		"Error":            r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "admin", data)
}

// Setup wizard
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodPost {
		s.handleSetupPost(w, r)
		return
	}

	// Get interfaces for setup
	interfaces, _ := s.network.ListInterfaces(ctx)

	// Filter to physical interfaces
	var physical []interface{}
	for _, iface := range interfaces {
		if iface.Type == "ethernet" || iface.Type == "wifi" {
			physical = append(physical, iface)
		}
	}

	// Get WiFi interfaces separately
	wifiIfaces, _ := s.wifi.ListWiFiInterfaces(ctx)

	data := map[string]interface{}{
		"Interfaces":     physical,
		"WiFiInterfaces": wifiIfaces,
		"Config":         s.config,
		"Step":           r.URL.Query().Get("step"),
	}

	s.renderTemplate(w, "setup", data)
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	step := r.FormValue("step")

	switch step {
	case "wan":
		s.config.WANInterface = r.FormValue("wan_interface")
		s.config.WANMode = r.FormValue("wan_mode")
		if s.config.WANMode == "static" {
			s.config.WANStaticIP = r.FormValue("wan_static_ip")
			s.config.WANStaticGateway = r.FormValue("wan_gateway")
			s.config.WANStaticDNS = r.FormValue("wan_dns")
		}

	case "lan":
		s.config.LANBridge = r.FormValue("lan_bridge")
		s.config.LANAddresses = []string{r.FormValue("lan_address")}
		ports := r.Form["lan_ports"]
		s.config.LANPorts = ports

	case "dhcp":
		s.config.DHCPEnabled = r.FormValue("dhcp_enabled") == "on"
		s.config.DHCPStart = r.FormValue("dhcp_start")
		s.config.DHCPEnd = r.FormValue("dhcp_end")
		s.config.DHCPLease = r.FormValue("dhcp_lease")

	case "dns":
		s.config.DNSEnabled = r.FormValue("dns_enabled") == "on"
		upstreamStr := r.FormValue("dns_upstream")
		s.config.DNSUpstream = strings.Split(strings.ReplaceAll(upstreamStr, " ", ""), ",")
		s.config.DNSLocalZone = r.FormValue("dns_local_zone")

	case "apply":
		// Apply all settings
		if err := s.applySetup(ctx); err != nil {
			http.Redirect(w, r, "/admin/setup?error="+err.Error(), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin?msg=Setup+completed", http.StatusSeeOther)
		return
	}

	// Save config after each step
	s.saveConfig()

	// Redirect to next step
	nextStep := map[string]string{
		"wan":  "lan",
		"lan":  "dhcp",
		"dhcp": "dns",
		"dns":  "apply",
	}

	http.Redirect(w, r, "/admin/setup?step="+nextStep[step], http.StatusSeeOther)
}

func (s *Server) applySetup(ctx context.Context) error {
	// Enable IP forwarding
	if err := s.network.EnableIPForward(ctx); err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %w", err)
	}

	// Configure network (netplan)
	netplanCfg := s.network.GenerateNetplanConfig(
		s.config.WANInterface,
		s.config.WANMode,
		s.config.WANStaticIP,
		s.config.WANStaticGateway,
		s.config.WANStaticDNS,
		s.config.LANBridge,
		s.config.LANPorts,
		s.config.LANAddresses,
	)
	if err := s.network.ApplyNetplan(ctx, netplanCfg); err != nil {
		return fmt.Errorf("failed to apply netplan: %w", err)
	}

	// Configure dnsmasq
	if s.config.DNSEnabled || s.config.DHCPEnabled {
		if err := s.dns.WriteConfig(s.config); err != nil {
			return fmt.Errorf("failed to write dnsmasq config: %w", err)
		}
		if err := s.dns.WriteHosts(s.config.DNSEntries); err != nil {
			return fmt.Errorf("failed to write DNS hosts: %w", err)
		}
		if err := s.dns.Restart(ctx); err != nil {
			return fmt.Errorf("failed to restart dnsmasq: %w", err)
		}
		s.dns.Enable(ctx)
	}

	return nil
}

// Interfaces
func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	interfaces, _ := s.network.ListInterfaces(ctx)
	routes, _ := s.network.GetRoutes(ctx)

	data := map[string]interface{}{
		"Interfaces": interfaces,
		"Routes":     routes,
		"Config":     s.config,
		"Message":    r.URL.Query().Get("msg"),
		"Error":      r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "interfaces", data)
}

func (s *Server) handleConfigureInterface(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/interfaces", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	iface := r.FormValue("interface")
	action := r.FormValue("action")

	var err error
	switch action {
	case "up":
		err = s.network.SetInterfaceUp(ctx, iface)
	case "down":
		err = s.network.SetInterfaceDown(ctx, iface)
	case "renew":
		err = s.network.RenewDHCP(ctx, iface)
	}

	if err != nil {
		http.Redirect(w, r, "/admin/interfaces?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/interfaces?msg=Interface+"+action, http.StatusSeeOther)
}

// DNS
func (s *Server) handleDNS(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	entries, _ := s.dns.GetDNSMappings()
	status, _ := s.dns.GetStatus(ctx, s.config)

	data := map[string]interface{}{
		"Entries": entries,
		"Status":  status,
		"Config":  s.config,
		"Message": r.URL.Query().Get("msg"),
		"Error":   r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "dns", data)
}

func (s *Server) handleDNSAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/dns", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	hostname := r.FormValue("hostname")
	ip := r.FormValue("ip")

	// Also add to config
	s.config.DNSEntries = append(s.config.DNSEntries, config.DNSEntry{
		Hostname: hostname,
		IP:       ip,
	})
	s.saveConfig()

	if err := s.dns.AddDNSEntry(ctx, hostname, ip); err != nil {
		http.Redirect(w, r, "/admin/dns?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/dns?msg=Entry+added", http.StatusSeeOther)
}

func (s *Server) handleDNSRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/dns", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	hostname := r.FormValue("hostname")

	// Remove from config
	var newEntries []config.DNSEntry
	for _, e := range s.config.DNSEntries {
		if e.Hostname != hostname {
			newEntries = append(newEntries, e)
		}
	}
	s.config.DNSEntries = newEntries
	s.saveConfig()

	if err := s.dns.RemoveDNSEntry(ctx, hostname); err != nil {
		http.Redirect(w, r, "/admin/dns?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/dns?msg=Entry+removed", http.StatusSeeOther)
}

// DHCP
func (s *Server) handleDHCP(w http.ResponseWriter, r *http.Request) {
	leases, _ := s.dns.GetLeases()

	data := map[string]interface{}{
		"Leases":       leases,
		"Reservations": s.config.DHCPReservations,
		"Config":       s.config,
		"Message":      r.URL.Query().Get("msg"),
		"Error":        r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "dhcp", data)
}

func (s *Server) handleDHCPReservationAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/dhcp", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	name := r.FormValue("name")
	mac := r.FormValue("mac")
	ip := r.FormValue("ip")

	if err := s.dns.AddDHCPReservation(ctx, s.config, name, mac, ip); err != nil {
		http.Redirect(w, r, "/admin/dhcp?error="+err.Error(), http.StatusSeeOther)
		return
	}

	s.saveConfig()
	s.dns.WriteConfig(s.config)
	s.dns.Reload(ctx)

	http.Redirect(w, r, "/admin/dhcp?msg=Reservation+added", http.StatusSeeOther)
}

func (s *Server) handleDHCPReservationRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/dhcp", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	mac := r.FormValue("mac")

	if err := s.dns.RemoveDHCPReservation(ctx, s.config, mac); err != nil {
		http.Redirect(w, r, "/admin/dhcp?error="+err.Error(), http.StatusSeeOther)
		return
	}

	s.saveConfig()
	s.dns.WriteConfig(s.config)
	s.dns.Reload(ctx)

	http.Redirect(w, r, "/admin/dhcp?msg=Reservation+removed", http.StatusSeeOther)
}

// WireGuard
func (s *Server) handleWireGuard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	var status interface{}
	if s.config.WireGuard != nil {
		status, _ = s.wireguard.GetFullStatus(ctx, s.config.WireGuard)
	}

	data := map[string]interface{}{
		"Status":  status,
		"Config":  s.config.WireGuard,
		"Message": r.URL.Query().Get("msg"),
		"Error":   r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "wireguard", data)
}

func (s *Server) handleWireGuardPeerAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/wireguard", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	name := r.FormValue("name")
	endpoint := r.FormValue("endpoint")

	if s.config.WireGuard == nil {
		http.Redirect(w, r, "/admin/wireguard?error=WireGuard+not+configured", http.StatusSeeOther)
		return
	}

	// Get server's public key
	status, err := s.wireguard.GetStatus(ctx)
	if err != nil {
		http.Redirect(w, r, "/admin/wireguard?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Generate client config
	clientConfig, err := s.wireguard.GenerateClientConfig(ctx, s.config.WireGuard, name, endpoint, status.PublicKey)
	if err != nil {
		http.Redirect(w, r, "/admin/wireguard?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Add peer to server config
	peer := config.WireGuardPeer{
		Name:       name,
		PublicKey:  clientConfig.PublicKey,
		AllowedIPs: []string{clientConfig.Address},
	}

	if err := s.wireguard.AddPeer(ctx, s.config.WireGuard, peer); err != nil {
		http.Redirect(w, r, "/admin/wireguard?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Save config and apply
	s.saveConfig()
	s.wireguard.WriteConfig(s.config.WireGuard, endpoint)
	s.wireguard.SyncConfig(ctx)

	// Store client config for download (in query param for simplicity)
	http.Redirect(w, r, "/admin/wireguard?msg=Peer+added&client_config="+clientConfig.Config, http.StatusSeeOther)
}

func (s *Server) handleWireGuardPeerRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/wireguard", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	pubKey := r.FormValue("public_key")

	if err := s.wireguard.RemovePeer(ctx, s.config.WireGuard, pubKey); err != nil {
		http.Redirect(w, r, "/admin/wireguard?error="+err.Error(), http.StatusSeeOther)
		return
	}

	s.saveConfig()
	s.wireguard.WriteConfig(s.config.WireGuard, "")
	s.wireguard.SyncConfig(ctx)

	http.Redirect(w, r, "/admin/wireguard?msg=Peer+removed", http.StatusSeeOther)
}

func (s *Server) handleWireGuardToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/wireguard", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	action := r.FormValue("action")

	var err error
	switch action {
	case "start":
		err = s.wireguard.Start(ctx)
	case "stop":
		err = s.wireguard.Stop(ctx)
	case "restart":
		err = s.wireguard.Restart(ctx)
	case "enable":
		err = s.wireguard.Enable(ctx)
		if err == nil {
			s.config.WireGuard.Enabled = true
			s.saveConfig()
		}
	case "disable":
		err = s.wireguard.Disable(ctx)
		if err == nil {
			s.config.WireGuard.Enabled = false
			s.saveConfig()
		}
	}

	if err != nil {
		http.Redirect(w, r, "/admin/wireguard?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/wireguard?msg=WireGuard+"+action, http.StatusSeeOther)
}

// WiFi
func (s *Server) handleWiFi(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	status, _ := s.wifi.GetStatus(ctx, s.config.WiFiInterfaces)
	wifiIfaces, _ := s.wifi.ListWiFiInterfaces(ctx)

	data := map[string]interface{}{
		"Status":          status,
		"Config":          s.config.WiFiInterfaces,
		"AvailableIfaces": wifiIfaces,
		"Message":         r.URL.Query().Get("msg"),
		"Error":           r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "wifi", data)
}

func (s *Server) handleWiFiConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/wifi", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	iface := r.FormValue("interface")
	ssid := r.FormValue("ssid")
	password := r.FormValue("password")
	channel, _ := strconv.Atoi(r.FormValue("channel"))
	band := r.FormValue("band")
	mode := r.FormValue("mode")
	bridge := r.FormValue("bridge")
	hidden := r.FormValue("hidden") == "on"
	enabled := r.FormValue("enabled") == "on"

	wifiCfg := &config.WiFiInterface{
		Interface: iface,
		Enabled:   enabled,
		SSID:      ssid,
		Password:  password,
		Channel:   channel,
		Band:      band,
		Mode:      mode,
		Bridge:    bridge,
		Hidden:    hidden,
	}

	// Update or add to config
	found := false
	for i, w := range s.config.WiFiInterfaces {
		if w.Interface == iface {
			s.config.WiFiInterfaces[i] = *wifiCfg
			found = true
			break
		}
	}
	if !found {
		s.config.WiFiInterfaces = append(s.config.WiFiInterfaces, *wifiCfg)
	}

	s.saveConfig()

	if err := s.wifi.ConfigureInterface(ctx, wifiCfg); err != nil {
		http.Redirect(w, r, "/admin/wifi?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/wifi?msg=WiFi+configured", http.StatusSeeOther)
}

func (s *Server) handleWiFiToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/wifi", http.StatusSeeOther)
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()
	iface := r.FormValue("interface")
	action := r.FormValue("action")

	var err error
	switch action {
	case "start":
		err = s.wifi.Start(ctx, iface)
	case "stop":
		err = s.wifi.Stop(ctx, iface)
	case "restart":
		err = s.wifi.Restart(ctx, iface)
	}

	if err != nil {
		http.Redirect(w, r, "/admin/wifi?error="+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/wifi?msg=WiFi+"+action, http.StatusSeeOther)
}

// Firewall
func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"PortForwards":  s.config.PortForwards,
		"FirewallRules": s.config.FirewallRules,
		"Config":        s.config,
		"Message":       r.URL.Query().Get("msg"),
		"Error":         r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "firewall", data)
}

func (s *Server) handlePortForwardAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/firewall", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	protocol := r.FormValue("protocol")
	extPort, _ := strconv.Atoi(r.FormValue("external_port"))
	intIP := r.FormValue("internal_ip")
	intPort, _ := strconv.Atoi(r.FormValue("internal_port"))
	enabled := r.FormValue("enabled") == "on"

	forward := config.PortForward{
		Name:         name,
		Enabled:      enabled,
		Protocol:     protocol,
		ExternalPort: extPort,
		InternalIP:   intIP,
		InternalPort: intPort,
	}

	s.config.PortForwards = append(s.config.PortForwards, forward)
	s.saveConfig()

	// TODO: Apply iptables rule

	http.Redirect(w, r, "/admin/firewall?msg=Port+forward+added", http.StatusSeeOther)
}

func (s *Server) handlePortForwardRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/firewall", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")

	var newForwards []config.PortForward
	for _, f := range s.config.PortForwards {
		if f.Name != name {
			newForwards = append(newForwards, f)
		}
	}
	s.config.PortForwards = newForwards
	s.saveConfig()

	// TODO: Remove iptables rule

	http.Redirect(w, r, "/admin/firewall?msg=Port+forward+removed", http.StatusSeeOther)
}

// Settings
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Config":  s.config,
		"Message": r.URL.Query().Get("msg"),
		"Error":   r.URL.Query().Get("error"),
	}

	s.renderTemplate(w, "settings", data)
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	// Update general settings from form
	s.config.ListenAddr = r.FormValue("listen_addr")

	s.saveConfig()

	http.Redirect(w, r, "/admin/settings?msg=Settings+saved", http.StatusSeeOther)
}

// API handlers
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	defaultIface, _ := s.network.GetDefaultInterface(ctx)

	status := map[string]interface{}{
		"version":          s.version,
		"ipForwardEnabled": s.network.IsIPForwardEnabled(),
		"wanInterface":     s.config.WANInterface,
		"defaultInterface": defaultIface,
	}

	if s.config.DNSEnabled {
		status["dnsStatus"], _ = s.dns.GetStatus(ctx, s.config)
	}

	if s.config.WireGuard != nil && s.config.WireGuard.Enabled {
		status["wireguardStatus"], _ = s.wireguard.GetFullStatus(ctx, s.config.WireGuard)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleAPIInterfaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	interfaces, err := s.network.ListInterfaces(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"interfaces": interfaces,
	})
}

func (s *Server) handleAPILeases(w http.ResponseWriter, r *http.Request) {
	leases, err := s.dns.GetLeases()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update device tracking for each lease with a valid hostname
	if s.statsStore != nil {
		ctx := r.Context()
		for _, lease := range leases {
			if lease.Hostname != "*" && lease.Hostname != "" {
				_ = s.statsStore.UpdateDeviceSeen(ctx, lease.MAC, lease.IP, lease.Hostname)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"leases": leases,
	})
}
