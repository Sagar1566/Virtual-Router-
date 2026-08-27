package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/acme"
	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/dns"
	"github.com/iodesystems/ubuntu-router/internal/logger"
	"github.com/iodesystems/ubuntu-router/internal/network"
	"github.com/iodesystems/ubuntu-router/internal/notifications"
	"github.com/iodesystems/ubuntu-router/internal/qos"
	"github.com/iodesystems/ubuntu-router/internal/rollback"
	"github.com/iodesystems/ubuntu-router/internal/stats"
	"github.com/iodesystems/ubuntu-router/internal/systemd"
	"github.com/iodesystems/ubuntu-router/internal/wireguard"
)

// JSON API handlers for the React frontend

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": message,
	})
}

// writeSuccess writes a JSON success response
func writeSuccess(w http.ResponseWriter, message string) {
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": message,
	})
}

// deriveSubnetFromRange attempts to derive a subnet CIDR from a DHCP range
func deriveSubnetFromRange(start, end string) string {
	// Simple approach: assume /24 subnet based on start IP
	parts := strings.Split(start, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return ""
}

// /api/config - GET/POST router config
func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		writeJSON(w, s.config)
		return
	}

	if r.Method == http.MethodPost {
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Apply updates (simplified)
		if addr, ok := updates["listenAddr"].(string); ok {
			s.config.ListenAddr = addr
		}
		if addrs, ok := updates["webListenAddresses"].([]interface{}); ok {
			s.config.WebListenAddresses = make([]string, 0, len(addrs))
			for _, a := range addrs {
				if str, ok := a.(string); ok {
					s.config.WebListenAddresses = append(s.config.WebListenAddresses, str)
				}
			}
		}

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Config updated")
		return
	}

	_ = ctx
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/routes - GET routing table
func (s *Server) handleAPIRoutes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Check for lookup parameter - returns which route handles a specific destination
	lookupIP := r.URL.Query().Get("lookup")
	if lookupIP != "" {
		result, err := s.network.LookupRoute(ctx, lookupIP)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, result)
		return
	}

	routes, err := s.network.GetRoutes(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"routes": routes,
	})
}

// /api/interfaces/state - POST set interface up/down
func (s *Server) handleAPIInterfaceState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Name string `json:"name"`
		Up   bool   `json:"up"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var err error
	if req.Up {
		err = s.network.SetInterfaceUp(ctx, req.Name)
	} else {
		err = s.network.SetInterfaceDown(ctx, req.Name)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "Interface state updated")
}

// /api/dns/status - GET DNS status
func (s *Server) handleAPIDNSStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	status, err := s.dns.GetStatus(ctx, s.config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, status)
}

// /api/dns/entries - GET/POST DNS entries
func (s *Server) handleAPIDNSEntries(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		entries, err := s.statsStore.GetDNSEntries(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Convert to API format
		result := make([]map[string]string, 0, len(entries))
		for _, e := range entries {
			result = append(result, map[string]string{
				"hostname": e.Hostname,
				"ip":       e.IP,
			})
		}
		writeJSON(w, map[string]interface{}{
			"entries": result,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Hostname string `json:"hostname"`
			IP       string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Add to SQLite
		if err := s.statsStore.AddDNSEntry(ctx, stats.DNSEntry{
			Hostname: req.Hostname,
			IP:       req.IP,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Regenerate dnsmasq hosts file and reload
		if err := s.regenerateDNSHosts(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "DNS entry added")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/dns/entries/delete - POST delete DNS entry
func (s *Server) handleAPIDNSEntriesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Remove from SQLite
	if err := s.statsStore.DeleteDNSEntry(ctx, req.Hostname); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Regenerate dnsmasq hosts file and reload
	if err := s.regenerateDNSHosts(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "DNS entry removed")
}

// /api/dns/settings - GET/POST DNS settings (upstream servers)
func (s *Server) handleAPIDNSSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		writeJSON(w, map[string]interface{}{
			"upstream_dns":   s.config.DNSUpstream,
			"dns_local_zone": s.config.DNSLocalZone,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			UpstreamDNS  []string `json:"upstream_dns"`
			DNSLocalZone string   `json:"dns_local_zone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Update config
		if len(req.UpstreamDNS) > 0 {
			s.config.DNSUpstream = req.UpstreamDNS
		}
		if req.DNSLocalZone != "" {
			s.config.DNSLocalZone = req.DNSLocalZone
		}
		s.saveConfig()

		// Regenerate DNS config and reload
		if err := s.dns.WriteDNSConfig(s.config); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to write DNS config: "+err.Error())
			return
		}
		if err := s.dns.Reload(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to reload dnsmasq: "+err.Error())
			return
		}

		writeSuccess(w, "DNS settings updated")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/dhcp/reservations - GET/POST reservations
func (s *Server) handleAPIDHCPReservations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		reservations, err := s.statsStore.GetDHCPReservations(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Convert to API format
		result := make([]map[string]string, 0, len(reservations))
		for _, r := range reservations {
			result = append(result, map[string]string{
				"name": r.Name,
				"mac":  r.MAC,
				"ip":   r.IP,
			})
		}
		writeJSON(w, map[string]interface{}{
			"reservations": result,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name string `json:"name"`
			MAC  string `json:"mac"`
			IP   string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Add to SQLite
		if err := s.statsStore.AddDHCPReservation(ctx, stats.DHCPReservation{
			Name: req.Name,
			MAC:  req.MAC,
			IP:   req.IP,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Regenerate dnsmasq config and reload
		if err := s.regenerateDHCPConfig(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Reservation added")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/dhcp/reservations/delete - POST delete reservation
func (s *Server) handleAPIDHCPReservationsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Delete from SQLite
	if err := s.statsStore.DeleteDHCPReservation(ctx, req.MAC); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Regenerate DHCP config and reload
	if err := s.regenerateDHCPConfig(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "Reservation removed")
}

// /api/dhcp/renew - POST renew DHCP lease on interface
func (s *Server) handleAPIDHCPRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.network.RenewDHCP(ctx, req.Interface); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "DHCP lease renewed")
}

// /api/dhcp/settings - GET/POST DHCP settings
func (s *Server) handleAPIDHCPSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		gateway := s.getGatewayIP()
		start := s.config.DHCPStart
		end := s.config.DHCPEnd

		// Check if current settings conflict with WAN and suggest better defaults
		if s.config.WANInterface != "" {
			wanIP := s.getWANIP(ctx)
			if wanIP != "" && gateway != "" && network.SubnetsConflict(wanIP, gateway) {
				gateway, start, end = network.CalculateLANSubnet(wanIP)
			}
		}

		writeJSON(w, map[string]interface{}{
			"enabled":    s.config.DHCPEnabled,
			"start":      start,
			"end":        end,
			"lease":      s.config.DHCPLease,
			"gateway":    gateway,
			"lan_bridge": s.config.LANBridge,
		})

	case http.MethodPost:
		var req struct {
			Enabled bool   `json:"enabled"`
			Start   string `json:"start"`
			End     string `json:"end"`
			Lease   string `json:"lease"`
			Gateway string `json:"gateway"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Update config
		s.config.DHCPEnabled = req.Enabled
		if req.Start != "" {
			s.config.DHCPStart = req.Start
		}
		if req.End != "" {
			s.config.DHCPEnd = req.End
		}
		if req.Lease != "" {
			s.config.DHCPLease = req.Lease
		}

		// Update LAN address if gateway changed
		if req.Gateway != "" && req.Gateway != s.getGatewayIP() {
			s.config.LANAddresses = []string{req.Gateway + "/24"}
			// Apply the gateway IP to the bridge (add without flushing to preserve connectivity)
			if s.config.LANBridge != "" {
				s.runner.Run(ctx, "ip", "addr", "add", req.Gateway+"/24", "dev", s.config.LANBridge)
				s.runner.Run(ctx, "ip", "link", "set", s.config.LANBridge, "up")
			}
		}

		// Save config
		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		// Write DHCP config and reload dnsmasq
		if err := s.dns.WriteDHCPConfig(s.config); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to write DHCP config: "+err.Error())
			return
		}
		s.dns.Reload(ctx)

		writeSuccess(w, "DHCP settings updated")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// getGatewayIP extracts the gateway IP from LANAddresses
func (s *Server) getGatewayIP() string {
	if len(s.config.LANAddresses) > 0 {
		return strings.Split(s.config.LANAddresses[0], "/")[0]
	}
	return ""
}

// getWANIP returns the WAN interface's IP address (without CIDR)
func (s *Server) getWANIP(ctx context.Context) string {
	if s.config.WANInterface == "" {
		return ""
	}
	out, err := s.runner.Run(ctx, "ip", "-4", "addr", "show", s.config.WANInterface)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "/")[0]
			}
		}
	}
	return ""
}

// /api/wireguard/status - GET WireGuard status
func (s *Server) handleAPIWireGuardStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if s.config.WireGuard == nil {
		writeJSON(w, nil)
		return
	}

	status, err := s.wireguard.GetFullStatus(ctx, s.config.WireGuard)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, status)
}

// /api/wireguard/settings - GET/POST WireGuard settings
func (s *Server) handleAPIWireGuardSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Ensure WireGuard config exists in memory
	if s.config.WireGuard == nil {
		s.config.WireGuard = &config.WireGuardConfig{
			Interface:  "wg0",
			ListenPort: 51820,
			Address:    "10.0.0.1/24",
			ConfigPath: "/etc/wireguard/wg0.conf",
		}
	}

	switch r.Method {
	case http.MethodGet:
		// Check if config file exists
		configExists := s.wireguard.ConfigExists()

		// Get public key if we have a private key
		publicKey := ""
		if s.config.WireGuard.PrivateKey != "" {
			if pk, err := s.wireguard.GetPublicKeyFromPrivate(ctx, s.config.WireGuard.PrivateKey); err == nil {
				publicKey = pk
			}
		}

		// Get VPN server IP from address
		vpnServerIP := ""
		if s.config.WireGuard.Address != "" {
			vpnServerIP = strings.Split(s.config.WireGuard.Address, "/")[0]
		}

		// Build available subnets list with descriptions
		type subnetInfo struct {
			Subnet      string `json:"subnet"`
			Description string `json:"description"`
			Source      string `json:"source"`
		}
		availableSubnets := []subnetInfo{}

		// Add LAN addresses
		for _, addr := range s.config.LANAddresses {
			availableSubnets = append(availableSubnets, subnetInfo{
				Subnet:      addr,
				Description: fmt.Sprintf("LAN Bridge (%s)", s.config.LANBridge),
				Source:      "lan",
			})
		}

		// Add DHCP range as subnet
		if s.config.DHCPEnabled && s.config.DHCPStart != "" && s.config.DHCPEnd != "" {
			// Try to derive subnet from DHCP range
			dhcpSubnet := deriveSubnetFromRange(s.config.DHCPStart, s.config.DHCPEnd)
			if dhcpSubnet != "" {
				availableSubnets = append(availableSubnets, subnetInfo{
					Subnet:      dhcpSubnet,
					Description: fmt.Sprintf("DHCP Range (%s - %s)", s.config.DHCPStart, s.config.DHCPEnd),
					Source:      "dhcp",
				})
			}
		}

		// Add WireGuard VPN subnet
		if s.config.WireGuard.Address != "" {
			availableSubnets = append(availableSubnets, subnetInfo{
				Subnet:      s.config.WireGuard.Address,
				Description: "WireGuard VPN Clients",
				Source:      "wireguard",
			})
		}

		// Add P2P tunnel subnets
		if s.config.WireGuardP2P != nil {
			for _, tunnel := range s.config.WireGuardP2P.Tunnels {
				if tunnel.Address != "" {
					availableSubnets = append(availableSubnets, subnetInfo{
						Subnet:      tunnel.Address,
						Description: fmt.Sprintf("Site-to-Site: %s", tunnel.Name),
						Source:      "site-to-site",
					})
				}
				for _, subnet := range tunnel.RemoteSubnets {
					availableSubnets = append(availableSubnets, subnetInfo{
						Subnet:      subnet,
						Description: fmt.Sprintf("Remote LAN: %s", tunnel.Name),
						Source:      "site-to-site-remote",
					})
				}
			}
		}

		writeJSON(w, map[string]interface{}{
			"enabled":           s.config.WireGuard.Enabled,
			"interface":         s.config.WireGuard.Interface,
			"listen_port":       s.config.WireGuard.ListenPort,
			"address":           s.config.WireGuard.Address,
			"dns":               s.config.WireGuard.DNS,
			"public_key":        publicKey,
			"config_exists":     configExists,
			"config_path":       s.config.WireGuard.ConfigPath,
			"route_all_traffic": s.config.WireGuard.RouteAllTraffic,
			"route_lan":         s.config.WireGuard.RouteLAN,
			"lan_subnets":       s.config.WireGuard.LANSubnets,
			"vpn_server_ip":     vpnServerIP,
			"upstream_dns":      s.config.DNSUpstream,
			"available_subnets": availableSubnets,
		})

	case http.MethodPost:
		var req struct {
			Enabled         bool     `json:"enabled"`
			Address         string   `json:"address"`
			ListenPort      int      `json:"listen_port"`
			DNS             string   `json:"dns"`
			RouteAllTraffic *bool    `json:"route_all_traffic"`
			RouteLAN        *bool    `json:"route_lan"`
			LANSubnets      []string `json:"lan_subnets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Update config
		if req.Address != "" {
			s.config.WireGuard.Address = req.Address
		}
		if req.ListenPort > 0 {
			s.config.WireGuard.ListenPort = req.ListenPort
		}
		if req.DNS != "" {
			s.config.WireGuard.DNS = req.DNS
		}
		if req.RouteAllTraffic != nil {
			s.config.WireGuard.RouteAllTraffic = *req.RouteAllTraffic
		}
		if req.RouteLAN != nil {
			s.config.WireGuard.RouteLAN = *req.RouteLAN
		}
		if req.LANSubnets != nil {
			s.config.WireGuard.LANSubnets = req.LANSubnets
		}
		s.config.WireGuard.Enabled = req.Enabled

		// Ensure config file exists with defaults
		created, err := s.wireguard.EnsureConfigWithContext(ctx, s.config.WireGuard, s.config.WANInterface)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to create config: "+err.Error())
			return
		}

		// If config already existed, rewrite it with updated settings
		if !created {
			if err := s.wireguard.WriteConfigWithWAN(s.config.WireGuard, s.config.WANInterface); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
				return
			}
		}

		// Save to main config
		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		msg := "WireGuard settings saved"
		if created {
			msg = "WireGuard config created with new key pair"
		}
		writeSuccess(w, msg)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/wireguard/peers - GET/POST peers
func (s *Server) handleAPIWireGuardPeers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if s.config.WireGuard == nil {
		writeError(w, http.StatusBadRequest, "WireGuard not configured")
		return
	}

	if r.Method == http.MethodGet {
		writeJSON(w, map[string]interface{}{
			"peers": s.config.WireGuard.Peers,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name           string `json:"name"`
			ServerEndpoint string `json:"serverEndpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Get server's public key
		status, err := s.wireguard.GetStatus(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		clientConfig, err := s.wireguard.GenerateClientConfig(ctx, s.config.WireGuard, req.Name, req.ServerEndpoint, status.PublicKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		peer := config.WireGuardPeer{
			Name:       req.Name,
			PublicKey:  clientConfig.PublicKey,
			AllowedIPs: []string{clientConfig.Address},
		}

		if err := s.wireguard.AddPeer(ctx, s.config.WireGuard, peer); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.saveConfig()
		s.wireguard.WriteConfig(s.config.WireGuard, req.ServerEndpoint)
		s.wireguard.SyncConfig(ctx)

		writeJSON(w, map[string]interface{}{
			"success":      true,
			"message":      "Peer added",
			"clientConfig": clientConfig.Config,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/wireguard/peers/delete - POST delete peer
func (s *Server) handleAPIWireGuardPeersDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.wireguard.RemovePeer(ctx, s.config.WireGuard, req.PublicKey); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.saveConfig()
	s.wireguard.WriteConfig(s.config.WireGuard, "")
	s.wireguard.SyncConfig(ctx)

	writeSuccess(w, "Peer removed")
}

// /api/wireguard/control - POST start/stop/restart
func (s *Server) handleAPIWireGuardControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var err error
	switch req.Action {
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
	default:
		writeError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "WireGuard "+req.Action)
}

// /api/wireguard/p2p/status - GET all P2P tunnel statuses
func (s *Server) handleAPIWireGuardP2PStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if s.config.WireGuardP2P == nil {
		writeJSON(w, map[string]interface{}{
			"tunnels": []interface{}{},
		})
		return
	}

	statuses, err := s.wireguardP2P.GetAllStatus(ctx, s.config.WireGuardP2P.Tunnels)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"tunnels": statuses,
	})
}

// /api/wireguard/p2p/tunnels - GET list / POST create
func (s *Server) handleAPIWireGuardP2PTunnels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Ensure P2P config exists
	if s.config.WireGuardP2P == nil {
		s.config.WireGuardP2P = &config.WireGuardP2PConfig{
			Tunnels: []config.WireGuardP2PTunnel{},
		}
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]interface{}{
			"tunnels": s.config.WireGuardP2P.Tunnels,
		})

	case http.MethodPost:
		var tunnel config.WireGuardP2PTunnel
		if err := json.NewDecoder(r.Body).Decode(&tunnel); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Prepare the tunnel (allocate interface, port, generate keys)
		if err := s.wireguardP2P.PrepareNewTunnel(ctx, &tunnel, s.config.WireGuardP2P.Tunnels); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Write config file
		if err := s.wireguardP2P.WriteConfig(&tunnel); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
			return
		}

		// Add to config
		s.config.WireGuardP2P.Tunnels = append(s.config.WireGuardP2P.Tunnels, tunnel)

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"message": "Tunnel created",
			"tunnel":  tunnel,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/wireguard/p2p/tunnels/{id}... - GET/PUT/DELETE tunnel, control, remote-config
func (s *Server) handleAPIWireGuardP2PTunnel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Parse path: /api/wireguard/p2p/tunnels/{id} or /api/wireguard/p2p/tunnels/{id}/control etc
	path := strings.TrimPrefix(r.URL.Path, "/api/wireguard/p2p/tunnels/")
	parts := strings.SplitN(path, "/", 2)
	tunnelID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if s.config.WireGuardP2P == nil {
		writeError(w, http.StatusNotFound, "P2P not configured")
		return
	}

	// Find tunnel
	var tunnel *config.WireGuardP2PTunnel
	var tunnelIdx int
	for i := range s.config.WireGuardP2P.Tunnels {
		if s.config.WireGuardP2P.Tunnels[i].ID == tunnelID {
			tunnel = &s.config.WireGuardP2P.Tunnels[i]
			tunnelIdx = i
			break
		}
	}

	if tunnel == nil {
		writeError(w, http.StatusNotFound, "Tunnel not found")
		return
	}

	switch action {
	case "":
		// CRUD on tunnel itself
		switch r.Method {
		case http.MethodGet:
			status, _ := s.wireguardP2P.GetTunnelStatus(ctx, tunnel)
			writeJSON(w, map[string]interface{}{
				"tunnel": tunnel,
				"status": status,
			})

		case http.MethodPut:
			var updates config.WireGuardP2PTunnel
			if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			// Update fields - preserve existing values if not provided
			updates.ID = tunnel.ID
			if updates.Interface == "" {
				updates.Interface = tunnel.Interface
			}
			if updates.ListenPort == 0 {
				updates.ListenPort = tunnel.ListenPort
			}
			if updates.PrivateKey == "" {
				updates.PrivateKey = tunnel.PrivateKey
			}
			if updates.PresharedKey == "" {
				updates.PresharedKey = tunnel.PresharedKey
			}
			if updates.RemotePublicKey == "" {
				updates.RemotePublicKey = tunnel.RemotePublicKey
			}
			if updates.RemoteEndpoint == "" {
				updates.RemoteEndpoint = tunnel.RemoteEndpoint
			}
			if updates.Address == "" {
				updates.Address = tunnel.Address
			}
			if updates.Name == "" {
				updates.Name = tunnel.Name
			}
			if updates.PersistentKeepalive == 0 {
				updates.PersistentKeepalive = tunnel.PersistentKeepalive
			}
			// Preserve subnets if not explicitly set (nil means not provided)
			if updates.LocalSubnets == nil {
				updates.LocalSubnets = tunnel.LocalSubnets
			}
			if updates.RemoteSubnets == nil {
				updates.RemoteSubnets = tunnel.RemoteSubnets
			}
			if updates.ForcedSubnets == nil {
				updates.ForcedSubnets = tunnel.ForcedSubnets
			}

			s.config.WireGuardP2P.Tunnels[tunnelIdx] = updates

			// Rewrite config
			if err := s.wireguardP2P.WriteConfig(&updates); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
				return
			}

			if err := s.saveConfig(); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
				return
			}

			writeSuccess(w, "Tunnel updated")

		case http.MethodDelete:
			// Stop tunnel if running
			if s.wireguardP2P.IsRunning(ctx, tunnel.Interface) {
				s.wireguardP2P.StopTunnel(ctx, tunnel.Interface)
			}

			// Delete config file
			s.wireguardP2P.DeleteConfig(tunnel.Interface)

			// Remove from config
			s.config.WireGuardP2P.Tunnels = append(
				s.config.WireGuardP2P.Tunnels[:tunnelIdx],
				s.config.WireGuardP2P.Tunnels[tunnelIdx+1:]...,
			)

			if err := s.saveConfig(); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
				return
			}

			writeSuccess(w, "Tunnel deleted")

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}

	case "control":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Check if there's already a pending change for start/restart/enable
		needsRollback := req.Action == "start" || req.Action == "restart" || req.Action == "enable"
		if needsRollback && s.rollback.IsPending() {
			writeJSON(w, rollback.MakeErrorResponse(fmt.Errorf("another change is pending confirmation")))
			return
		}

		// Create snapshot for operations that modify network routing
		var snapshot *rollback.Snapshot
		if needsRollback {
			var snapErr error
			snapshot, snapErr = rollback.CreateSnapshot(s.fs, s.configPath, s.config)
			if snapErr != nil {
				writeError(w, http.StatusInternalServerError, "Failed to create rollback snapshot: "+snapErr.Error())
				return
			}
		}

		var err error
		var validation *wireguard.ValidationResult

		switch req.Action {
		case "start":
			// Rewrite config with safe subnets before starting
			validation, err = s.wireguardP2P.WriteConfigSafe(tunnel, s.config)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
				return
			}
			if !validation.Valid {
				writeError(w, http.StatusBadRequest, "Tunnel configuration is invalid")
				return
			}
			err = s.wireguardP2P.StartTunnel(ctx, tunnel.Interface)
			if err == nil {
				// Enable systemd auto-start and persist the enabled state
				_ = s.wireguardP2P.EnableTunnel(ctx, tunnel.Interface)
				s.config.WireGuardP2P.Tunnels[tunnelIdx].Enabled = true
				s.saveConfig()
			}

		case "stop":
			err = s.wireguardP2P.StopTunnel(ctx, tunnel.Interface)
			if err == nil {
				// Disable systemd auto-start and persist the disabled state
				_ = s.wireguardP2P.DisableTunnel(ctx, tunnel.Interface)
				s.config.WireGuardP2P.Tunnels[tunnelIdx].Enabled = false
				s.saveConfig()
			}

		case "restart":
			// Rewrite config with safe subnets before restarting
			validation, err = s.wireguardP2P.WriteConfigSafe(tunnel, s.config)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
				return
			}
			if !validation.Valid {
				writeError(w, http.StatusBadRequest, "Tunnel configuration is invalid")
				return
			}
			err = s.wireguardP2P.RestartTunnel(ctx, tunnel.Interface)
			if err == nil {
				// Ensure systemd auto-start is enabled and persist the state
				_ = s.wireguardP2P.EnableTunnel(ctx, tunnel.Interface)
				s.config.WireGuardP2P.Tunnels[tunnelIdx].Enabled = true
				s.saveConfig()
			}

		case "enable":
			err = s.wireguardP2P.EnableTunnel(ctx, tunnel.Interface)
			if err == nil {
				s.config.WireGuardP2P.Tunnels[tunnelIdx].Enabled = true
				s.saveConfig()
			}

		case "disable":
			err = s.wireguardP2P.DisableTunnel(ctx, tunnel.Interface)
			if err == nil {
				s.config.WireGuardP2P.Tunnels[tunnelIdx].Enabled = false
				s.saveConfig()
			}

		default:
			writeError(w, http.StatusBadRequest, "Invalid action")
			return
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Start rollback timer for network-modifying operations
		if needsRollback && snapshot != nil {
			if rbErr := s.rollback.BeginChange("p2p_tunnel_"+req.Action, snapshot, rollback.DefaultTimeout); rbErr != nil {
				writeError(w, http.StatusInternalServerError, "Failed to start rollback timer: "+rbErr.Error())
				return
			}
		}

		// Build response with validation and rollback info
		response := s.rollback.MakeChangeResponse(true, "Tunnel "+req.Action)
		if validation != nil {
			if len(validation.FilteredSubnets) > 0 {
				response.Message = fmt.Sprintf("Tunnel %s. %d subnet(s) filtered: %v. Confirm within 30s or changes will be reverted.",
					req.Action, len(validation.FilteredSubnets), validation.FilteredSubnets)
			} else if needsRollback {
				response.Message = fmt.Sprintf("Tunnel %s. Confirm within 30 seconds or changes will be reverted.", req.Action)
			}
		}
		writeJSON(w, response)

	case "validate":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Return full validation result with conflicts and warnings
		result := s.wireguardP2P.ValidateTunnelConfig(tunnel, s.config)
		writeJSON(w, result)

	case "remote-config":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		endpoint := r.URL.Query().Get("endpoint")
		if endpoint == "" {
			writeError(w, http.StatusBadRequest, "endpoint parameter required")
			return
		}

		remoteConfig, err := s.wireguardP2P.GenerateRemoteConfig(ctx, tunnel, endpoint)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, remoteConfig)

	default:
		writeError(w, http.StatusNotFound, "Unknown action")
	}
}

// /api/wireguard/p2p/generate-keys - POST generate new key pair
func (s *Server) handleAPIWireGuardP2PGenerateKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	keyPair, err := s.wireguardP2P.GenerateKeyPair(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, keyPair)
}

// /api/wireguard/p2p/import-config - POST import WireGuard config file
func (s *Server) handleAPIWireGuardP2PImportConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Name   string `json:"name"`
		Config string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Config == "" {
		writeError(w, http.StatusBadRequest, "Config is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Parse the config
	parsed, err := s.wireguardP2P.ParseConfig(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to parse config: "+err.Error())
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Initialize P2P config if needed
	if s.config.WireGuardP2P == nil {
		s.config.WireGuardP2P = &config.WireGuardP2PConfig{
			Tunnels: []config.WireGuardP2PTunnel{},
		}
	}

	// Create tunnel from parsed config
	tunnel, err := s.wireguardP2P.CreateTunnelFromConfig(ctx, req.Name, parsed, s.config.WireGuardP2P.Tunnels)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create tunnel: "+err.Error())
		return
	}

	// Add to configuration (store original remote subnets)
	s.config.WireGuardP2P.Tunnels = append(s.config.WireGuardP2P.Tunnels, *tunnel)
	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Write the WireGuard config file with SAFE subnets (filtered to avoid conflicts)
	// This validates and removes conflicting subnets automatically
	validation, err := s.wireguardP2P.WriteConfigSafe(tunnel, s.config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
		return
	}

	// If remote endpoint points to self, don't start (validation.Valid will be false)
	if !validation.Valid {
		writeJSON(w, map[string]interface{}{
			"success":    false,
			"message":    "Tunnel configuration is invalid and cannot function",
			"tunnel":     tunnel,
			"parsed":     parsed,
			"validation": validation,
		})
		return
	}

	// Start the tunnel - it's now safe because conflicting subnets were filtered
	started := false
	var startErr string
	if err := s.wireguardP2P.StartTunnel(ctx, tunnel.Interface); err != nil {
		startErr = err.Error()
		log.Printf("Warning: Tunnel created but failed to start: %v", err)
	} else {
		started = true
	}

	// Build response message
	message := "Tunnel created and started"
	if len(validation.FilteredSubnets) > 0 {
		message = fmt.Sprintf("Tunnel created. %d subnet(s) were filtered to preserve local connectivity: %v",
			len(validation.FilteredSubnets), validation.FilteredSubnets)
	}
	if !started {
		message = "Tunnel created but failed to start: " + startErr
	}

	writeJSON(w, map[string]interface{}{
		"success":    true,
		"message":    message,
		"tunnel":     tunnel,
		"parsed":     parsed,
		"validation": validation,
		"started":    started,
		"startError": startErr,
	})
}

// /api/wifi/status - GET WiFi status
func (s *Server) handleAPIWiFiStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	status, err := s.wifi.GetStatus(ctx, s.config.WiFiInterfaces)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, status)
}

// /api/wifi/interfaces - GET available WiFi interfaces
func (s *Server) handleAPIWiFiInterfaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	ifaces, err := s.wifi.ListWiFiInterfaces(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"interfaces": ifaces,
	})
}

// /api/wifi/config - GET config for a specific WiFi interface
func (s *Server) handleAPIWiFiConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	iface := r.URL.Query().Get("interface")
	if iface == "" {
		writeError(w, http.StatusBadRequest, "interface parameter required")
		return
	}

	for _, wifi := range s.config.WiFiInterfaces {
		if wifi.Interface == iface {
			writeJSON(w, wifi)
			return
		}
	}

	writeError(w, http.StatusNotFound, "WiFi interface not found")
}

// /api/wifi/configure - POST configure AP
func (s *Server) handleAPIWiFiConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var cfg config.WiFiInterface
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check if this interface is already configured as a WAN
	for _, wan := range s.config.WANs {
		if wan.Interface == cfg.Interface {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Interface %s is already configured as a WAN. Remove it from WAN list first.", cfg.Interface))
			return
		}
	}

	// Auto-generate SSID if not provided
	if cfg.SSID == "" {
		cfg.SSID = config.GenerateSSID()
		logger.Info("WiFi configure: auto-generated SSID %s for %s", cfg.SSID, cfg.Interface)
	}

	logger.Info("WiFi configure: %s SSID=%s channel=%d band=%s enabled=%v",
		cfg.Interface, cfg.SSID, cfg.Channel, cfg.Band, cfg.Enabled)

	// Update or add to config
	found := false
	for i, w := range s.config.WiFiInterfaces {
		if w.Interface == cfg.Interface {
			s.config.WiFiInterfaces[i] = cfg
			found = true
			break
		}
	}
	if !found {
		s.config.WiFiInterfaces = append(s.config.WiFiInterfaces, cfg)
	}

	s.saveConfig()

	if err := s.wifi.ConfigureInterface(ctx, &cfg); err != nil {
		logger.Error("WiFi configure failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("WiFi configure: completed for %s", cfg.Interface)
	writeSuccess(w, "WiFi configured")
}

// /api/wifi/control - POST start/stop/restart
func (s *Server) handleAPIWiFiControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
		Action    string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logger.Info("WiFi control: %s on %s", req.Action, req.Interface)

	var err error
	switch req.Action {
	case "start":
		err = s.wifi.Start(ctx, req.Interface)
	case "stop":
		err = s.wifi.Stop(ctx, req.Interface)
	case "restart":
		err = s.wifi.Restart(ctx, req.Interface)
	default:
		logger.Warn("WiFi control: invalid action %s", req.Action)
		writeError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	if err != nil {
		logger.Error("WiFi control failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("WiFi control: %s completed on %s", req.Action, req.Interface)
	writeSuccess(w, "WiFi "+req.Action)
}

// /api/wifi/delete - POST delete WiFi interface configuration
func (s *Server) handleAPIWiFiDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Interface == "" {
		writeError(w, http.StatusBadRequest, "interface is required")
		return
	}

	// Remove from config
	newInterfaces := make([]config.WiFiInterface, 0)
	for _, w := range s.config.WiFiInterfaces {
		if w.Interface != req.Interface {
			newInterfaces = append(newInterfaces, w)
		}
	}
	s.config.WiFiInterfaces = newInterfaces
	s.saveConfig()

	// Deconfigure the interface (stop service, remove files)
	if err := s.wifi.DeconfigureInterface(ctx, req.Interface); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "WiFi interface deleted")
}

// /api/wifi/stations - GET connected stations
func (s *Server) handleAPIWiFiStations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	iface := r.URL.Query().Get("interface")
	if iface == "" {
		writeError(w, http.StatusBadRequest, "interface parameter required")
		return
	}

	stations, err := s.wifi.GetStations(ctx, iface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"stations": stations,
	})
}

// /api/wifi/cards - GET all WiFi cards with capabilities
func (s *Server) handleAPIWiFiCards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Check if user wants only AP-capable cards
	apOnly := r.URL.Query().Get("ap_only") == "true"

	var cards interface{}
	var err error

	if apOnly {
		cards, err = s.wifi.DetectAPCapableCards(ctx)
	} else {
		cards, err = s.wifi.GetAllWiFiCards(ctx)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"cards": cards,
	})
}

// /api/firewall/forwards - GET/POST port forwards
func (s *Server) handleAPIFirewallForwards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		// Get from SQLite
		forwards, err := s.statsStore.GetPortForwards(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{
			"portForwards": forwards,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name         string `json:"name"`
			Protocol     string `json:"protocol"`
			ExternalPort int    `json:"external_port"`
			InternalIP   string `json:"internal_ip"`
			InternalPort int    `json:"internal_port"`
			Enabled      bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Add to SQLite
		if err := s.statsStore.AddPortForward(ctx, stats.PortForward{
			Name:         req.Name,
			Protocol:     req.Protocol,
			ExternalPort: req.ExternalPort,
			InternalIP:   req.InternalIP,
			InternalPort: req.InternalPort,
			Enabled:      req.Enabled,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Port forward added")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/firewall/forwards/delete - POST delete port forward
func (s *Server) handleAPIFirewallForwardsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Delete from SQLite
	if err := s.statsStore.DeletePortForward(ctx, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "Port forward removed")
}

// /api/apply - POST apply all configuration
func (s *Server) handleAPIApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Check if there's already a pending change
	if s.rollback.IsPending() {
		writeJSON(w, rollback.MakeErrorResponse(fmt.Errorf("another change is pending confirmation")))
		return
	}

	// Create snapshot BEFORE making any changes
	snapshot, err := rollback.CreateSnapshot(s.fs, s.configPath, s.config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create rollback snapshot: "+err.Error())
		return
	}

	// Apply all configuration
	if err := s.applySetup(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start rollback timer - changes will revert in 30s unless confirmed
	if err := s.rollback.BeginChange("apply", snapshot, rollback.DefaultTimeout); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to start rollback timer: "+err.Error())
		return
	}

	// Return response indicating pending confirmation required
	writeJSON(w, s.rollback.MakeChangeResponse(true, "Configuration applied. Confirm within 30 seconds or changes will be reverted."))
}

// /api/config/pending - GET pending change status
func (s *Server) handleAPIConfigPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	status := s.rollback.GetPending()
	writeJSON(w, status)
}

// /api/config/confirm - POST confirm pending changes
func (s *Server) handleAPIConfigConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if err := s.rollback.Confirm(); err != nil {
		writeJSON(w, rollback.MakeErrorResponse(err))
		return
	}

	writeJSON(w, &rollback.ChangeResponse{
		Success: true,
		Message: "Changes confirmed and saved permanently.",
	})
}

// /api/config/cancel - POST cancel pending changes and rollback
func (s *Server) handleAPIConfigCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if err := s.rollback.Cancel(); err != nil {
		writeJSON(w, rollback.MakeErrorResponse(err))
		return
	}

	writeJSON(w, &rollback.ChangeResponse{
		Success: true,
		Message: "Changes cancelled and rolled back.",
	})
}

// /api/wan/status - GET WAN status
func (s *Server) handleAPIWANStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Get current WAN interface info
	var wanInterface *struct {
		Name      string   `json:"name"`
		State     string   `json:"state"`
		Addresses []string `json:"addresses"`
		Gateway   string   `json:"gateway"`
	}

	if s.config.WANInterface != "" {
		iface, err := s.network.GetInterface(ctx, s.config.WANInterface)
		if err == nil && iface != nil {
			var addrs []string
			for _, addr := range iface.Addresses {
				addrs = append(addrs, addr.IP)
			}

			// Find gateway from routes
			gateway := ""
			routes, _ := s.network.GetRoutes(ctx)
			for _, route := range routes {
				if route.Destination == "default" && route.Interface == s.config.WANInterface {
					gateway = route.Gateway
					break
				}
			}

			wanInterface = &struct {
				Name      string   `json:"name"`
				State     string   `json:"state"`
				Addresses []string `json:"addresses"`
				Gateway   string   `json:"gateway"`
			}{
				Name:      iface.Name,
				State:     iface.State,
				Addresses: addrs,
				Gateway:   gateway,
			}
		}
	}

	// Include WiFi status if mode is wifi
	var wifiStatus interface{}
	if s.config.WANMode == "wifi" && s.config.WANInterface != "" {
		status, _ := s.wifi.GetClientStatus(ctx, s.config.WANInterface)
		wifiStatus = status
	}

	writeJSON(w, map[string]interface{}{
		"configured": s.config.WANInterface != "",
		"interface":  wanInterface,
		"config": map[string]interface{}{
			"interface":     s.config.WANInterface,
			"mode":          s.config.WANMode,
			"staticIP":      s.config.WANStaticIP,
			"staticGateway": s.config.WANStaticGateway,
			"staticDNS":     s.config.WANStaticDNS,
			"wifiSSID":      s.config.WANWiFiSSID,
			"wifiSecurity":  s.config.WANWiFiSecurity,
		},
		"wifiStatus": wifiStatus,
	})
}

// /api/wan/detect - GET auto-detect WAN interface and mode
func (s *Server) handleAPIWANDetect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	iface, err := s.network.DetectWANInterface(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Detect mode: if interface has addresses and we have a gateway, likely DHCP
	mode := "dhcp"
	var currentIP, currentGateway string

	if len(iface.Addresses) > 0 {
		// Find IPv4 address
		for _, addr := range iface.Addresses {
			if addr.Family == "inet" {
				currentIP = fmt.Sprintf("%s/%d", addr.IP, addr.Prefix)
				break
			}
		}
	}

	// Check for default gateway via this interface
	routes, _ := s.network.GetRoutes(ctx)
	for _, route := range routes {
		if route.Destination == "default" && route.Interface == iface.Name {
			currentGateway = route.Gateway
			break
		}
	}

	writeJSON(w, map[string]interface{}{
		"interface":      iface.Name,
		"mode":           mode,
		"currentIP":      currentIP,
		"currentGateway": currentGateway,
		"hasAddress":     len(iface.Addresses) > 0,
	})
}

// /api/wan/configure - POST configure WAN
func (s *Server) handleAPIWANConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface     string `json:"interface"`
		Mode          string `json:"mode"`
		StaticIP      string `json:"static_ip"`
		StaticGateway string `json:"static_gateway"`
		StaticDNS     string `json:"static_dns"`
		// WiFi mode settings
		WiFiSSID     string `json:"wifi_ssid"`
		WiFiPassword string `json:"wifi_password"`
		WiFiSecurity string `json:"wifi_security"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check if there's already a pending change
	if s.rollback.IsPending() {
		writeJSON(w, rollback.MakeErrorResponse(fmt.Errorf("another change is pending confirmation")))
		return
	}

	// Validate interface exists
	iface, err := s.network.GetInterface(ctx, req.Interface)
	if err != nil || iface == nil {
		writeError(w, http.StatusBadRequest, "Interface not found: "+req.Interface)
		return
	}

	// Validate mode
	if req.Mode != "dhcp" && req.Mode != "static" && req.Mode != "pppoe" && req.Mode != "wifi" {
		writeError(w, http.StatusBadRequest, "Invalid mode. Must be dhcp, static, pppoe, or wifi")
		return
	}

	// For static mode, validate required fields
	if req.Mode == "static" {
		if req.StaticIP == "" {
			writeError(w, http.StatusBadRequest, "Static IP is required for static mode")
			return
		}
		if req.StaticGateway == "" {
			writeError(w, http.StatusBadRequest, "Gateway is required for static mode")
			return
		}
	}

	// PPPoE not yet implemented
	if req.Mode == "pppoe" {
		writeError(w, http.StatusNotImplemented, "PPPoE mode is not yet implemented")
		return
	}

	// Create snapshot BEFORE making any changes
	snapshot, err := rollback.CreateSnapshot(s.fs, s.configPath, s.config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create rollback snapshot: "+err.Error())
		return
	}

	// For WiFi mode, validate SSID and connect
	if req.Mode == "wifi" {
		if req.WiFiSSID == "" {
			writeError(w, http.StatusBadRequest, "WiFi SSID is required for wifi mode")
			return
		}

		// Connect to WiFi network
		security := req.WiFiSecurity
		if security == "" {
			if req.WiFiPassword == "" {
				security = "open"
			} else {
				security = "wpa2"
			}
		}

		logger.Info("WAN configure: connecting to WiFi %s on %s", req.WiFiSSID, req.Interface)
		if err := s.wifi.ConnectToNetwork(ctx, req.Interface, req.WiFiSSID, req.WiFiPassword, security); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to connect to WiFi: "+err.Error())
			return
		}

		// Create wpa_supplicant service for persistence
		if err := s.wifi.WriteWpaSupplicantService(req.Interface); err != nil {
			logger.Warn("Failed to create wpa_supplicant service: %v", err)
		} else {
			s.runner.Run(ctx, "systemctl", "daemon-reload")
			s.wifi.EnableWpaSupplicant(ctx, req.Interface)
		}
	}

	// Update config
	s.config.WANInterface = req.Interface
	s.config.WANMode = req.Mode
	s.config.WANStaticIP = req.StaticIP
	s.config.WANStaticGateway = req.StaticGateway
	s.config.WANStaticDNS = req.StaticDNS
	s.config.WANWiFiSSID = req.WiFiSSID
	s.config.WANWiFiPassword = req.WiFiPassword
	s.config.WANWiFiSecurity = req.WiFiSecurity

	// Generate and apply netplan configuration
	netplanConfig := s.network.GenerateNetplanConfig(
		s.config.WANInterface,
		s.config.WANMode,
		s.config.WANStaticIP,
		s.config.WANStaticGateway,
		s.config.WANStaticDNS,
		s.config.LANBridge,
		s.config.LANPorts,
		s.config.LANAddresses,
	)

	if err := s.network.ApplyNetplan(ctx, netplanConfig); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to apply network config: "+err.Error())
		return
	}

	// Save config
	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Start rollback timer - changes will revert in 30s unless confirmed
	if err := s.rollback.BeginChange("wan_configure", snapshot, rollback.DefaultTimeout); err != nil {
		// This shouldn't happen since we checked IsPending earlier, but handle it
		writeError(w, http.StatusInternalServerError, "Failed to start rollback timer: "+err.Error())
		return
	}

	// Return response indicating pending confirmation required
	writeJSON(w, s.rollback.MakeChangeResponse(true, "WAN configuration applied. Confirm within 30 seconds or changes will be reverted."))
}

// /api/system/dependencies - GET system dependencies status
func (s *Server) handleAPISystemDependencies(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	deps := s.deps.CheckAll(ctx)

	// Calculate summary
	var installed, missing, optional int
	for _, dep := range deps {
		if dep.Installed {
			installed++
		} else if dep.Required {
			missing++
		} else {
			optional++
		}
	}

	writeJSON(w, map[string]interface{}{
		"dependencies": deps,
		"summary": map[string]interface{}{
			"installed": installed,
			"missing":   missing,
			"optional":  optional,
			"ready":     missing == 0,
		},
	})
}

// /api/system/logs - GET application logs
func (s *Server) handleAPISystemLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	entries := logger.GetEntries(limit)
	writeJSON(w, map[string]interface{}{
		"entries": entries,
	})
}

// /api/system/shutdown - POST shutdown the system
func (s *Server) handleAPISystemShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Log the shutdown request
	logger.Info("System shutdown requested via API")

	// Send response before shutting down
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "System is shutting down...",
	})

	// Flush response
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Shutdown after a short delay to allow response to be sent
	// Use background context since request context will be canceled after response
	go func() {
		time.Sleep(1 * time.Second)
		_, err := s.runner.Run(context.Background(), "systemctl", "poweroff")
		if err != nil {
			logger.Error("Failed to shutdown: %v", err)
		}
	}()
}

// /api/system/reboot - POST reboot the system
func (s *Server) handleAPISystemReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Log the reboot request
	logger.Info("System reboot requested via API")

	// Send response before rebooting
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "System is rebooting...",
	})

	// Flush response
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Reboot after a short delay to allow response to be sent
	// Use background context since request context will be canceled after response
	go func() {
		time.Sleep(1 * time.Second)
		_, err := s.runner.Run(context.Background(), "systemctl", "reboot")
		if err != nil {
			logger.Error("Failed to reboot: %v", err)
		}
	}()
}

// /api/health/check - GET/POST run health checks
func (s *Server) handleAPIHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Run all health checks
	results, err := s.health.RunAll(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get summary
	summary := s.health.GetSummary()

	writeJSON(w, map[string]interface{}{
		"results": results,
		"summary": map[string]interface{}{
			"total":    summary.Total,
			"ok":       summary.OK,
			"warnings": summary.Warnings,
			"errors":   summary.Errors,
			"skipped":  summary.Skipped,
			"ready":    summary.Errors == 0,
		},
	})
}

// /api/wifi/debug - GET debug info for WiFi
func (s *Server) handleAPIWiFiDebug(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	iface := r.URL.Query().Get("interface")
	if iface == "" {
		iface = "wlan0"
	}

	result := map[string]interface{}{
		"interface": iface,
	}

	// Check hostapd binary location
	hostapdPath, _ := s.runner.Run(ctx, "which", "hostapd")
	result["hostapd_path"] = strings.TrimSpace(string(hostapdPath))

	// Check if hostapd exists at various locations
	paths := []string{"/usr/sbin/hostapd", "/usr/bin/hostapd", "/sbin/hostapd"}
	existsAt := []string{}
	for _, p := range paths {
		if _, err := s.fs.Stat(p); err == nil {
			existsAt = append(existsAt, p)
		}
	}
	result["hostapd_exists_at"] = existsAt

	// Read service file
	servicePath := fmt.Sprintf("/etc/systemd/system/hostapd-%s.service", iface)
	serviceContent, err := s.fs.ReadFile(servicePath)
	if err != nil {
		result["service_file_error"] = err.Error()
	} else {
		result["service_file"] = string(serviceContent)
	}

	// Read config file
	configPath := fmt.Sprintf("/etc/hostapd/hostapd-%s.conf", iface)
	configContent, err := s.fs.ReadFile(configPath)
	if err != nil {
		result["config_file_error"] = err.Error()
	} else {
		result["config_file"] = string(configContent)
	}

	// Get journal logs
	journalOut, _ := s.runner.Run(ctx, "journalctl", "-u", fmt.Sprintf("hostapd-%s", iface), "-n", "50", "--no-pager")
	result["journal_logs"] = string(journalOut)

	// Get service status
	statusOut, _ := s.runner.Run(ctx, "systemctl", "status", fmt.Sprintf("hostapd-%s", iface), "--no-pager")
	result["service_status"] = string(statusOut)

	writeJSON(w, result)
}

// /api/wan/wifi/scan - GET scan for WiFi networks
func (s *Server) handleAPIWANWiFiScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	iface := r.URL.Query().Get("interface")
	if iface == "" {
		// Try to find first WiFi interface
		interfaces, err := s.wifi.ListWiFiInterfaces(ctx)
		if err != nil || len(interfaces) == 0 {
			writeError(w, http.StatusBadRequest, "No WiFi interface found. Specify ?interface=wlan0")
			return
		}
		iface = interfaces[0]
	}

	networks, err := s.wifi.ScanNetworks(ctx, iface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Scan failed: "+err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"interface": iface,
		"networks":  networks,
	})
}

// /api/wan/wifi/status - GET WiFi client connection status
func (s *Server) handleAPIWANWiFiStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	iface := r.URL.Query().Get("interface")
	if iface == "" {
		iface = s.config.WANInterface
	}
	if iface == "" {
		writeError(w, http.StatusBadRequest, "No WiFi interface configured")
		return
	}

	status, err := s.wifi.GetClientStatus(ctx, iface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"interface": iface,
		"status":    status,
		"config": map[string]interface{}{
			"ssid":     s.config.WANWiFiSSID,
			"security": s.config.WANWiFiSecurity,
		},
	})
}

// /api/wan/wifi/connect - POST connect to WiFi network
func (s *Server) handleAPIWANWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
		SSID      string `json:"ssid"`
		Password  string `json:"password"`
		Security  string `json:"security"` // open, wep, wpa, wpa2, wpa3
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.SSID == "" {
		writeError(w, http.StatusBadRequest, "SSID is required")
		return
	}

	// Default interface
	if req.Interface == "" {
		interfaces, err := s.wifi.ListWiFiInterfaces(ctx)
		if err != nil || len(interfaces) == 0 {
			writeError(w, http.StatusBadRequest, "No WiFi interface found")
			return
		}
		req.Interface = interfaces[0]
	}

	// Default security
	if req.Security == "" {
		if req.Password == "" {
			req.Security = "open"
		} else {
			req.Security = "wpa2"
		}
	}

	logger.Info("WiFi WAN connect: interface=%s ssid=%s security=%s", req.Interface, req.SSID, req.Security)

	// Connect to network
	if err := s.wifi.ConnectToNetwork(ctx, req.Interface, req.SSID, req.Password, req.Security); err != nil {
		logger.Error("WiFi WAN connect failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update config
	s.config.WANInterface = req.Interface
	s.config.WANMode = "wifi"
	s.config.WANWiFiSSID = req.SSID
	s.config.WANWiFiPassword = req.Password
	s.config.WANWiFiSecurity = req.Security
	s.saveConfig()

	// Create wpa_supplicant service for persistence
	if err := s.wifi.WriteWpaSupplicantService(req.Interface); err != nil {
		logger.Warn("Failed to create wpa_supplicant service: %v", err)
	} else {
		s.runner.Run(ctx, "systemctl", "daemon-reload")
		s.wifi.EnableWpaSupplicant(ctx, req.Interface)
	}

	logger.Info("WiFi WAN connect: success")
	writeSuccess(w, "Connected to "+req.SSID)
}

// /api/wan/wifi/disconnect - POST disconnect from WiFi network
func (s *Server) handleAPIWANWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Interface == "" {
		req.Interface = s.config.WANInterface
	}
	if req.Interface == "" {
		writeError(w, http.StatusBadRequest, "No WiFi interface specified")
		return
	}

	logger.Info("WiFi WAN disconnect: interface=%s", req.Interface)

	if err := s.wifi.DisconnectNetwork(ctx, req.Interface); err != nil {
		logger.Error("WiFi WAN disconnect failed: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Disable wpa_supplicant service
	s.wifi.DisableWpaSupplicant(ctx, req.Interface)

	logger.Info("WiFi WAN disconnect: success")
	writeSuccess(w, "Disconnected from WiFi")
}

// /api/multiwan/status - GET multi-WAN status
func (s *Server) handleAPIMultiWANStatus(w http.ResponseWriter, r *http.Request) {
	status := s.multiwan.GetStatus()
	writeJSON(w, status)
}

// /api/multiwan/configure - POST configure multi-WAN (updates WANs list in config)
func (s *Server) handleAPIMultiWANConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		WANs          []config.WANConfig `json:"wans"`
		FailbackDelay int                `json:"failback_delay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate and set defaults for WANs
	for i := range req.WANs {
		wan := &req.WANs[i]
		if wan.Interface == "" {
			writeError(w, http.StatusBadRequest, "WAN interface is required")
			return
		}
		// Generate ID if not provided
		if wan.ID == "" {
			wan.ID = config.GenerateWANID()
		}
		if wan.Name == "" {
			wan.Name = wan.Interface
		}
		// Set health check defaults
		if wan.HealthCheckInterval <= 0 {
			wan.HealthCheckInterval = 10
		}
		if wan.HealthCheckTimeout <= 0 {
			wan.HealthCheckTimeout = 5
		}
		if wan.HealthCheckRetries <= 0 {
			wan.HealthCheckRetries = 3
		}
	}

	if req.FailbackDelay <= 0 {
		req.FailbackDelay = 60
	}

	// Update config
	s.config.WANs = req.WANs
	s.config.FailbackDelay = req.FailbackDelay
	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Reconfigure and restart the multi-WAN manager
	s.multiwan.Stop()
	s.multiwan.Configure(s.config)

	if len(req.WANs) > 0 {
		if err := s.multiwan.Start(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to start multi-WAN: "+err.Error())
			return
		}
	}

	logger.Info("WAN configuration updated: wans=%d failback_delay=%d", len(req.WANs), req.FailbackDelay)
	writeSuccess(w, "WAN configuration updated")
}

// /api/multiwan/switch - POST force switch to specific WAN
func (s *Server) handleAPIMultiWANSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Interface string `json:"interface"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Interface == "" {
		writeError(w, http.StatusBadRequest, "interface is required")
		return
	}

	if err := s.multiwan.ForceSwitch(ctx, req.Interface); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Multi-WAN: forced switch to %s", req.Interface)
	writeSuccess(w, "Switched to "+req.Interface)
}

// /api/wans - GET list all WANs, POST add a new WAN
func (s *Server) handleAPIWANs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		// Return list of configured WANs with status
		status := s.multiwan.GetStatus()

		// Add interface stats to each WAN in the status
		if status != nil && len(status.WANs) > 0 {
			for i := range status.WANs {
				ifaceStats, err := s.stats.GetInterfaceStats(ctx, status.WANs[i].Interface)
				if err == nil && ifaceStats != nil {
					// Add stats as extra fields (we'll use a map to add these)
				}
			}
		}

		// Get interface stats, routes, and DNS for all WANs
		ifaceStats := make(map[string]map[string]interface{})
		ifaceRoutes := make(map[string][]string)
		ifaceDNS := make(map[string][]string)

		for _, wan := range s.config.WANs {
			// Get interface stats
			stats, err := s.stats.GetInterfaceStats(ctx, wan.Interface)
			if err == nil && stats != nil {
				ifaceStats[wan.Interface] = map[string]interface{}{
					"rx_bytes":   stats.RxBytes,
					"tx_bytes":   stats.TxBytes,
					"rx_packets": stats.RxPackets,
					"tx_packets": stats.TxPackets,
				}
			}

			// Get routes for this interface
			routeOutput, err := s.runner.Run(ctx, "ip", "route", "show", "dev", wan.Interface)
			if err == nil {
				routes := strings.Split(strings.TrimSpace(string(routeOutput)), "\n")
				var cleanRoutes []string
				for _, r := range routes {
					r = strings.TrimSpace(r)
					if r != "" {
						cleanRoutes = append(cleanRoutes, r)
					}
				}
				ifaceRoutes[wan.Interface] = cleanRoutes
			}

			// Get DNS servers (try resolvectl first, fall back to static config)
			dnsOutput, err := s.runner.Run(ctx, "resolvectl", "dns", wan.Interface)
			if err == nil {
				// Parse: "Link 2 (eth0): 192.168.1.1 8.8.8.8"
				line := strings.TrimSpace(string(dnsOutput))
				if idx := strings.Index(line, "):"); idx != -1 {
					dnsStr := strings.TrimSpace(line[idx+2:])
					if dnsStr != "" {
						ifaceDNS[wan.Interface] = strings.Fields(dnsStr)
					}
				}
			} else if wan.StaticDNS != "" {
				// Use static DNS from config
				ifaceDNS[wan.Interface] = strings.Split(wan.StaticDNS, ",")
			}
		}

		writeJSON(w, map[string]interface{}{
			"wans":            s.config.WANs,
			"failback_delay":  s.config.FailbackDelay,
			"status":          status,
			"interface_stats": ifaceStats,
			"interface_routes": ifaceRoutes,
			"interface_dns":   ifaceDNS,
		})

	case http.MethodPost:
		// Add a new WAN
		var wan config.WANConfig
		if err := json.NewDecoder(r.Body).Decode(&wan); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if wan.Interface == "" {
			writeError(w, http.StatusBadRequest, "interface is required")
			return
		}

		// Check if this interface is already configured as a WiFi AP
		for _, wifi := range s.config.WiFiInterfaces {
			if wifi.Interface == wan.Interface {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Interface %s is already configured as a WiFi AP. Remove it from WiFi AP list first.", wan.Interface))
				return
			}
		}

		// Generate ID if not provided
		if wan.ID == "" {
			wan.ID = config.GenerateWANID()
		}
		if wan.Name == "" {
			wan.Name = wan.Interface
		}

		// Set defaults
		if wan.HealthCheckInterval <= 0 {
			wan.HealthCheckInterval = 10
		}
		if wan.HealthCheckTimeout <= 0 {
			wan.HealthCheckTimeout = 5
		}
		if wan.HealthCheckRetries <= 0 {
			wan.HealthCheckRetries = 3
		}
		if wan.Priority <= 0 {
			// Set priority to be after all existing WANs
			maxPriority := 0
			for _, w := range s.config.WANs {
				if w.Priority > maxPriority {
					maxPriority = w.Priority
				}
			}
			wan.Priority = maxPriority + 1
		}

		// If WiFi mode, connect to the network
		if wan.Mode == "wifi" && wan.WiFiSSID != "" {
			security := wan.WiFiSecurity
			if security == "" {
				security = "wpa2"
			}
			if err := s.wifi.ConnectToNetwork(ctx, wan.Interface, wan.WiFiSSID, wan.WiFiPassword, security); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to connect to WiFi: "+err.Error())
				return
			}
			// Create wpa_supplicant service for persistence
			if err := s.wifi.WriteWpaSupplicantService(wan.Interface); err != nil {
				logger.Warn("Failed to create wpa_supplicant service: %v", err)
			} else {
				s.runner.Run(ctx, "systemctl", "daemon-reload")
				s.wifi.EnableWpaSupplicant(ctx, wan.Interface)
			}
		}

		// Add to config
		s.config.WANs = append(s.config.WANs, wan)
		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		// Reconfigure multi-WAN manager
		s.multiwan.Stop()
		s.multiwan.Configure(s.config)
		if err := s.multiwan.Start(ctx); err != nil {
			logger.Warn("Failed to restart multi-WAN: %v", err)
		}

		logger.Info("WAN added: %s (%s)", wan.Name, wan.Interface)
		writeJSON(w, wan)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/wans/{id} - GET/PUT/DELETE a specific WAN by ID
func (s *Server) handleAPIWANByID(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Extract ID from path: /api/wans/{id} or /api/wans/{id}/refresh-dhcp
	path := strings.TrimPrefix(r.URL.Path, "/api/wans/")
	path = strings.TrimSuffix(path, "/")

	// Handle /api/wans/{id}/refresh-dhcp
	if strings.HasSuffix(path, "/refresh-dhcp") {
		s.handleAPIWANRefreshDHCP(w, r)
		return
	}

	id := path
	if id == "" || id == "reorder" || id == "autodetect" {
		writeError(w, http.StatusBadRequest, "WAN ID is required")
		return
	}

	// Find the WAN by ID
	wanIndex := -1
	for i, wan := range s.config.WANs {
		if wan.ID == id {
			wanIndex = i
			break
		}
	}

	switch r.Method {
	case http.MethodGet:
		if wanIndex == -1 {
			writeError(w, http.StatusNotFound, "WAN not found")
			return
		}
		// Return the WAN with its status
		wan := s.config.WANs[wanIndex]
		status := s.multiwan.GetStatus()
		var wanStatus interface{}
		for _, ws := range status.WANs {
			if ws.Interface == wan.Interface {
				wanStatus = ws
				break
			}
		}
		writeJSON(w, map[string]interface{}{
			"wan":    wan,
			"status": wanStatus,
		})

	case http.MethodPut:
		if wanIndex == -1 {
			writeError(w, http.StatusNotFound, "WAN not found")
			return
		}
		// Update the WAN
		var wan config.WANConfig
		if err := json.NewDecoder(r.Body).Decode(&wan); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Preserve the ID
		wan.ID = id
		if wan.Interface == "" {
			writeError(w, http.StatusBadRequest, "interface is required")
			return
		}

		// Check if this interface is already configured as a WiFi AP
		for _, wifi := range s.config.WiFiInterfaces {
			if wifi.Interface == wan.Interface {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Interface %s is already configured as a WiFi AP. Remove it from WiFi AP list first.", wan.Interface))
				return
			}
		}

		if wan.Name == "" {
			wan.Name = wan.Interface
		}

		// If WiFi mode, connect to the network
		if wan.Mode == "wifi" && wan.WiFiSSID != "" {
			security := wan.WiFiSecurity
			if security == "" {
				security = "wpa2"
			}
			if err := s.wifi.ConnectToNetwork(ctx, wan.Interface, wan.WiFiSSID, wan.WiFiPassword, security); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to connect to WiFi: "+err.Error())
				return
			}
			// Create wpa_supplicant service for persistence
			if err := s.wifi.WriteWpaSupplicantService(wan.Interface); err != nil {
				logger.Warn("Failed to create wpa_supplicant service: %v", err)
			} else {
				s.runner.Run(ctx, "systemctl", "daemon-reload")
				s.wifi.EnableWpaSupplicant(ctx, wan.Interface)
			}
		}

		s.config.WANs[wanIndex] = wan
		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		// Reconfigure multi-WAN manager
		s.multiwan.Stop()
		s.multiwan.Configure(s.config)
		if err := s.multiwan.Start(ctx); err != nil {
			logger.Warn("Failed to restart multi-WAN: %v", err)
		}

		logger.Info("WAN updated: %s (%s)", wan.Name, wan.Interface)
		writeJSON(w, wan)

	case http.MethodDelete:
		if wanIndex == -1 {
			writeError(w, http.StatusNotFound, "WAN not found")
			return
		}
		// Remove the WAN
		removed := s.config.WANs[wanIndex]
		s.config.WANs = append(s.config.WANs[:wanIndex], s.config.WANs[wanIndex+1:]...)
		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		// Reconfigure multi-WAN manager
		s.multiwan.Stop()
		s.multiwan.Configure(s.config)
		if len(s.config.WANs) > 0 {
			if err := s.multiwan.Start(ctx); err != nil {
				logger.Warn("Failed to restart multi-WAN: %v", err)
			}
		}

		logger.Info("WAN removed: %s (%s)", removed.Name, removed.Interface)
		writeSuccess(w, "WAN removed")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/wans/reorder - POST reorder WANs by priority
func (s *Server) handleAPIWANsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		Order []string `json:"order"` // List of WAN IDs in desired order
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Order) != len(s.config.WANs) {
		writeError(w, http.StatusBadRequest, "order must contain all WAN IDs")
		return
	}

	// Build a map of ID to WAN config
	wanMap := make(map[string]config.WANConfig)
	for _, wan := range s.config.WANs {
		wanMap[wan.ID] = wan
	}

	// Reorder WANs based on the order list
	newWANs := make([]config.WANConfig, 0, len(req.Order))
	for i, id := range req.Order {
		wan, ok := wanMap[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown WAN ID: "+id)
			return
		}
		wan.Priority = i + 1
		newWANs = append(newWANs, wan)
	}

	s.config.WANs = newWANs
	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Reconfigure multi-WAN manager
	s.multiwan.Stop()
	s.multiwan.Configure(s.config)
	if len(s.config.WANs) > 0 {
		if err := s.multiwan.Start(ctx); err != nil {
			logger.Warn("Failed to restart multi-WAN: %v", err)
		}
	}

	logger.Info("WANs reordered: %v", req.Order)
	writeJSON(w, s.config.WANs)
}

// /api/wans/autodetect - POST auto-detect and add WAN interfaces
func (s *Server) handleAPIWANsAutoDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Get all interfaces
	interfaces, err := s.network.ListInterfaces(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get interfaces: "+err.Error())
		return
	}

	// Track already configured interfaces
	configured := make(map[string]bool)
	for _, wan := range s.config.WANs {
		configured[wan.Interface] = true
	}

	// Track interfaces used for WiFi AP
	wifiAPInterfaces := make(map[string]bool)
	for _, wifi := range s.config.WiFiInterfaces {
		wifiAPInterfaces[wifi.Interface] = true
	}

	// Find interfaces that could be WANs
	var added []config.WANConfig
	priority := len(s.config.WANs) + 1

	for _, iface := range interfaces {
		// Skip already configured, loopback, bridge, wireguard, and veth interfaces
		if configured[iface.Name] {
			continue
		}
		if iface.Name == "lo" || iface.Name == s.config.LANBridge {
			continue
		}
		if strings.HasPrefix(iface.Name, "wg") || strings.HasPrefix(iface.Name, "veth") {
			continue
		}
		if strings.HasPrefix(iface.Name, "br") || strings.HasPrefix(iface.Name, "docker") {
			continue
		}
		// Skip WiFi interfaces used for AP
		if wifiAPInterfaces[iface.Name] {
			continue
		}

		// Check if it's an ethernet or wifi interface
		isEthernet := iface.Type == "ethernet"
		isWiFi := iface.Type == "wifi" || strings.HasPrefix(iface.Name, "wl")

		if !isEthernet && !isWiFi {
			continue
		}

		// Create WAN config
		wan := config.WANConfig{
			ID:                  config.GenerateWANID(),
			Name:                iface.Name,
			Interface:           iface.Name,
			Enabled:             true,
			Priority:            priority,
			Mode:                "dhcp",
			HealthCheckInterval: 10,
			HealthCheckTimeout:  5,
			HealthCheckRetries:  3,
			HealthCheckTargets:  []string{"8.8.8.8", "1.1.1.1"},
		}

		if isWiFi {
			wan.Mode = "wifi"
		}

		s.config.WANs = append(s.config.WANs, wan)
		added = append(added, wan)
		priority++
		logger.Info("WAN auto-detected: %s (priority %d)", iface.Name, wan.Priority)
	}

	if len(added) == 0 {
		writeJSON(w, map[string]interface{}{
			"message": "No new WAN interfaces detected",
			"added":   []config.WANConfig{},
		})
		return
	}

	// Save config
	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Reconfigure multi-WAN manager
	s.multiwan.Stop()
	s.multiwan.Configure(s.config)
	if err := s.multiwan.Start(ctx); err != nil {
		logger.Warn("Failed to restart multi-WAN: %v", err)
	}

	writeJSON(w, map[string]interface{}{
		"message": fmt.Sprintf("Added %d WAN interface(s)", len(added)),
		"added":   added,
	})
}

// /api/wans/{id}/refresh-dhcp - POST refresh DHCP on a WAN interface
func (s *Server) handleAPIWANRefreshDHCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Extract ID from path: /api/wans/{id}/refresh-dhcp
	path := strings.TrimPrefix(r.URL.Path, "/api/wans/")
	path = strings.TrimSuffix(path, "/refresh-dhcp")
	id := path

	// Find the WAN by ID
	var wan *config.WANConfig
	for i := range s.config.WANs {
		if s.config.WANs[i].ID == id {
			wan = &s.config.WANs[i]
			break
		}
	}

	if wan == nil {
		writeError(w, http.StatusNotFound, "WAN not found")
		return
	}

	// Only works for DHCP mode
	if wan.Mode != "dhcp" && wan.Mode != "" {
		writeError(w, http.StatusBadRequest, "WAN is not in DHCP mode")
		return
	}

	iface := wan.Interface

	// Release current DHCP lease
	_, _ = s.runner.Run(ctx, "dhclient", "-r", iface)

	// Request new DHCP lease
	output, err := s.runner.Run(ctx, "dhclient", "-v", iface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to refresh DHCP: "+err.Error())
		return
	}

	logger.Info("DHCP refreshed on %s: %s", iface, string(output))

	// Get new IP info
	ipOutput, _ := s.runner.Run(ctx, "ip", "-4", "addr", "show", iface)

	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "DHCP lease refreshed on " + iface,
		"output":  string(ipOutput),
	})
}

// /api/health/fix - POST run a fixer
func (s *Server) handleAPIHealthFix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		FixerID string `json:"fixer_id"`
		DryRun  bool   `json:"dry_run,omitempty"` // Preview what the fixer would do
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.FixerID == "" {
		writeError(w, http.StatusBadRequest, "fixer_id is required")
		return
	}

	// Get fixer info for description
	fixers := s.health.GetFixers()
	fixer, exists := fixers[req.FixerID]
	if !exists {
		writeError(w, http.StatusNotFound, "fixer not found")
		return
	}

	modeStr := ""
	if req.DryRun {
		modeStr = " (DRY RUN)"
	}

	logger.Info("Health Fix: Starting fixer '%s'%s", req.FixerID, modeStr)
	logger.Info("Health Fix: Description: %s", fixer.Description)
	if fixer.RequiresRoot {
		logger.Info("Health Fix: This fixer requires root privileges")
	}

	if req.DryRun {
		// In dry-run mode, just log what would happen
		logger.Info("Health Fix: Would execute fixer '%s'", req.FixerID)
		logger.Info("Health Fix: Completed%s - no changes made", modeStr)
		writeJSON(w, map[string]interface{}{
			"success":     true,
			"dry_run":     true,
			"message":     "Dry run completed - no changes made",
			"description": fixer.Description,
		})
		return
	}

	if err := s.health.RunFixer(ctx, req.FixerID); err != nil {
		logger.Error("Health Fix: Failed to run fixer '%s': %v", req.FixerID, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Health Fix: Successfully completed fixer '%s'", req.FixerID)
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Fix applied successfully",
	})
}

// =============================================================================
// External DNS API handlers
// =============================================================================

// /api/external-dns/status - GET external DNS status
func (s *Server) handleAPIExternalDNSStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get public IP
	publicIP, err := s.getPublicIP()
	if err != nil {
		publicIP = ""
	}

	// Build zone statuses
	var zones []map[string]interface{}
	if s.config.ExternalDNS != nil {
		for _, zone := range s.config.ExternalDNS.Zones {
			zoneStatus := map[string]interface{}{
				"id":           zone.ID,
				"name":         zone.Name,
				"provider":     string(zone.Provider.Type),
				"ssl_enabled":  zone.SSLEnabled,
				"ssl_email":    zone.SSLEmail,
				"last_synced":  zone.LastSynced,
			}
			zones = append(zones, zoneStatus)
		}
	}

	// Build record statuses
	var records []map[string]interface{}
	if s.config.ExternalDNS != nil {
		for _, rec := range s.config.ExternalDNS.Records {
			recStatus := map[string]interface{}{
				"id":        rec.ID,
				"name":      rec.Name,
				"type":      rec.Type,
				"value":     rec.Value,
				"ttl":       rec.TTL,
				"zone_id":   rec.ZoneID,
				"auto_sync": rec.AutoSync,
			}
			records = append(records, recStatus)
		}
	}

	writeJSON(w, map[string]interface{}{
		"enabled":       s.config.ExternalDNS != nil && s.config.ExternalDNS.Enabled,
		"public_ip":     publicIP,
		"auto_sync_ip":  s.config.ExternalDNS != nil && s.config.ExternalDNS.AutoSyncIP,
		"sync_interval": s.getSyncInterval(),
		"zones":         zones,
		"records":       records,
	})
}

func (s *Server) getSyncInterval() int {
	if s.config.ExternalDNS != nil && s.config.ExternalDNS.SyncInterval > 0 {
		return s.config.ExternalDNS.SyncInterval
	}
	return 5 // default 5 minutes
}

// /api/external-dns/public-ip - GET/POST public IP
func (s *Server) handleAPIExternalDNSPublicIP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		publicIP, err := s.getPublicIP()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{
			"public_ip": publicIP,
		})
		return
	}

	if r.Method == http.MethodPost {
		// Force refresh public IP
		publicIP, err := s.detectPublicIP()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{
			"public_ip": publicIP,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/external-dns/zones - GET/POST zones
func (s *Server) handleAPIExternalDNSZones(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var zones []config.ExternalDNSZone
		if s.config.ExternalDNS != nil {
			zones = s.config.ExternalDNS.Zones
		}
		writeJSON(w, map[string]interface{}{
			"zones": zones,
		})
		return
	}

	if r.Method == http.MethodPost {
		var zone config.ExternalDNSZone
		if err := json.NewDecoder(r.Body).Decode(&zone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Generate ID if not provided
		if zone.ID == "" {
			zone.ID = fmt.Sprintf("zone-%d", time.Now().UnixNano())
		}

		// Initialize ExternalDNS config if needed
		if s.config.ExternalDNS == nil {
			s.config.ExternalDNS = &config.ExternalDNSConfig{
				Enabled: true,
			}
		}

		// Add zone
		s.config.ExternalDNS.Zones = append(s.config.ExternalDNS.Zones, zone)

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"zone":    zone,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/external-dns/zones/{id} - GET/PUT/DELETE zone
func (s *Server) handleAPIExternalDNSZone(w http.ResponseWriter, r *http.Request) {
	// Extract zone ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/external-dns/zones/")
	parts := strings.Split(path, "/")
	zoneID := parts[0]

	if zoneID == "" {
		writeError(w, http.StatusBadRequest, "zone ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		zone := s.findZone(zoneID)
		if zone == nil {
			writeError(w, http.StatusNotFound, "zone not found")
			return
		}
		writeJSON(w, zone)

	case http.MethodPut:
		var update config.ExternalDNSZone
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := s.updateZone(zoneID, update); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Zone updated")

	case http.MethodDelete:
		if err := s.deleteZone(zoneID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, "Zone deleted")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/external-dns/records - GET/POST records
func (s *Server) handleAPIExternalDNSRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var records []config.ExternalDNSRecord
		if s.config.ExternalDNS != nil {
			records = s.config.ExternalDNS.Records
		}
		writeJSON(w, map[string]interface{}{
			"records": records,
		})
		return
	}

	if r.Method == http.MethodPost {
		var rec config.ExternalDNSRecord
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Generate ID if not provided
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("rec-%d", time.Now().UnixNano())
		}

		// Default TTL
		if rec.TTL == 0 {
			rec.TTL = 300
		}

		// Initialize ExternalDNS config if needed
		if s.config.ExternalDNS == nil {
			s.config.ExternalDNS = &config.ExternalDNSConfig{
				Enabled: true,
			}
		}

		// Add record
		s.config.ExternalDNS.Records = append(s.config.ExternalDNS.Records, rec)

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"record":  rec,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/external-dns/records/{id} - GET/PUT/DELETE record
func (s *Server) handleAPIExternalDNSRecord(w http.ResponseWriter, r *http.Request) {
	// Extract record ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/external-dns/records/")
	parts := strings.Split(path, "/")
	recordID := parts[0]

	if recordID == "" {
		writeError(w, http.StatusBadRequest, "record ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rec := s.findRecord(recordID)
		if rec == nil {
			writeError(w, http.StatusNotFound, "record not found")
			return
		}
		writeJSON(w, rec)

	case http.MethodPut:
		var update config.ExternalDNSRecord
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := s.updateRecord(recordID, update); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Record updated")

	case http.MethodDelete:
		if err := s.deleteRecord(recordID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, "Record deleted")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/external-dns/sync - POST sync DNS records
func (s *Server) handleAPIExternalDNSSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	var req struct {
		RecordID string `json:"record_id,omitempty"` // Sync specific record, or all if empty
		DryRun   bool   `json:"dry_run,omitempty"`   // Preview what would happen without making changes
	}
	json.NewDecoder(r.Body).Decode(&req)

	results, err := s.syncDNSRecords(ctx, req.RecordID, req.DryRun)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"success": true,
		"dry_run": req.DryRun,
		"results": results,
	})
}

// /api/external-dns/discover-zones - GET discover available zones from provider
func (s *Server) handleAPIExternalDNSDiscoverZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var providerCfg config.DNSProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&providerCfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := dns.NewProvider(&providerCfg, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	zones, err := provider.ListZones()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"zones": zones,
	})
}

// /api/external-dns/ssl/request - POST request SSL certificate
func (s *Server) handleAPIExternalDNSSSLRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		ZoneID string `json:"zone_id"`
		DryRun bool   `json:"dry_run,omitempty"` // Preview what would happen
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	zone := s.findZone(req.ZoneID)
	if zone == nil {
		writeError(w, http.StatusNotFound, "zone not found")
		return
	}

	if !zone.SSLEnabled || zone.SSLEmail == "" {
		writeError(w, http.StatusBadRequest, "SSL not enabled or email not configured for this zone")
		return
	}

	modeStr := ""
	if req.DryRun {
		modeStr = " (DRY RUN)"
	}

	logger.Info("SSL Request: Starting certificate request for %s%s", zone.Name, modeStr)
	logger.Info("SSL Request: Using email %s for Let's Encrypt", zone.SSLEmail)
	logger.Info("SSL Request: DNS provider: %s", zone.Provider.Type)

	// Get certificate directory
	certDir := "/etc/ubuntu-router/certs"
	if s.config.ExternalDNS != nil && s.config.ExternalDNS.CertDir != "" {
		certDir = s.config.ExternalDNS.CertDir
	}
	logger.Info("SSL Request: Certificates will be stored in %s", certDir)

	if req.DryRun {
		// In dry-run mode, just log what would happen
		logger.Info("SSL Request: Would request certificate for domain: %s", zone.Name)
		logger.Info("SSL Request: Would use DNS-01 challenge via %s provider", zone.Provider.Type)
		logger.Info("SSL Request: Would store certificate at %s/live/%s/fullchain.pem", certDir, zone.Name)
		logger.Info("SSL Request: Completed%s - no certificate requested", modeStr)
		writeJSON(w, map[string]interface{}{
			"success":   true,
			"dry_run":   true,
			"message":   "Dry run completed - no certificate requested",
			"cert_path": fmt.Sprintf("%s/live/%s/fullchain.pem", certDir, zone.Name),
			"key_path":  fmt.Sprintf("%s/live/%s/privkey.pem", certDir, zone.Name),
		})
		return
	}

	// Request certificate
	acmeMgr := acme.NewManager(certDir, false) // production
	err := acmeMgr.RequestCertificate(zone, func(msg string) {
		logger.Info("SSL Request: %s", msg)
	})
	if err != nil {
		logger.Error("SSL Request: Failed to obtain certificate: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update zone with cert paths
	zone.CertPath = fmt.Sprintf("%s/live/%s/fullchain.pem", certDir, zone.Name)
	zone.KeyPath = fmt.Sprintf("%s/live/%s/privkey.pem", certDir, zone.Name)
	zone.LastSSLRenew = time.Now().Format(time.RFC3339)

	logger.Info("SSL Request: Certificate obtained successfully")
	logger.Info("SSL Request: Certificate path: %s", zone.CertPath)
	logger.Info("SSL Request: Key path: %s", zone.KeyPath)

	if err := s.updateZone(req.ZoneID, *zone); err != nil {
		logger.Error("SSL Request: Failed to save zone config: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("SSL Request: Completed successfully for %s", zone.Name)

	writeJSON(w, map[string]interface{}{
		"success":   true,
		"cert_path": zone.CertPath,
		"key_path":  zone.KeyPath,
	})
}

// /api/external-dns/ssl/status - GET SSL certificate status
func (s *Server) handleAPIExternalDNSSSLStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	zoneID := r.URL.Query().Get("zone_id")
	if zoneID == "" {
		writeError(w, http.StatusBadRequest, "zone_id required")
		return
	}

	zone := s.findZone(zoneID)
	if zone == nil {
		writeError(w, http.StatusNotFound, "zone not found")
		return
	}

	certDir := "/etc/ubuntu-router/certs"
	if s.config.ExternalDNS != nil && s.config.ExternalDNS.CertDir != "" {
		certDir = s.config.ExternalDNS.CertDir
	}

	acmeMgr := acme.NewManager(certDir, false)
	status := acmeMgr.GetCertStatus(zone.Name)

	var certInfo *acme.CertInfo
	if status.CertExists {
		certInfo, _ = acmeMgr.GetCertInfoForZone(zone.Name)
	}

	writeJSON(w, map[string]interface{}{
		"zone_id":     zoneID,
		"zone_name":   zone.Name,
		"cert_exists": status.CertExists,
		"cert_path":   status.CertPath,
		"key_path":    status.KeyPath,
		"expiry_info": status.ExpiryInfo,
		"cert_info":   certInfo,
	})
}

// /api/external-dns/settings - GET/POST settings
func (s *Server) handleAPIExternalDNSSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		settings := map[string]interface{}{
			"enabled":       false,
			"auto_sync_ip":  false,
			"sync_interval": 5,
			"public_ip":     "",
			"cert_dir":      "/etc/ubuntu-router/certs",
		}

		if s.config.ExternalDNS != nil {
			settings["enabled"] = s.config.ExternalDNS.Enabled
			settings["auto_sync_ip"] = s.config.ExternalDNS.AutoSyncIP
			if s.config.ExternalDNS.SyncInterval > 0 {
				settings["sync_interval"] = s.config.ExternalDNS.SyncInterval
			}
			settings["public_ip"] = s.config.ExternalDNS.PublicIP
			if s.config.ExternalDNS.CertDir != "" {
				settings["cert_dir"] = s.config.ExternalDNS.CertDir
			}
		}

		writeJSON(w, settings)
		return
	}

	if r.Method == http.MethodPost {
		var update struct {
			Enabled      *bool   `json:"enabled,omitempty"`
			AutoSyncIP   *bool   `json:"auto_sync_ip,omitempty"`
			SyncInterval *int    `json:"sync_interval,omitempty"`
			PublicIP     *string `json:"public_ip,omitempty"`
			CertDir      *string `json:"cert_dir,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Initialize if needed
		if s.config.ExternalDNS == nil {
			s.config.ExternalDNS = &config.ExternalDNSConfig{}
		}

		if update.Enabled != nil {
			s.config.ExternalDNS.Enabled = *update.Enabled
		}
		if update.AutoSyncIP != nil {
			s.config.ExternalDNS.AutoSyncIP = *update.AutoSyncIP
		}
		if update.SyncInterval != nil {
			s.config.ExternalDNS.SyncInterval = *update.SyncInterval
		}
		if update.PublicIP != nil {
			s.config.ExternalDNS.PublicIP = *update.PublicIP
		}
		if update.CertDir != nil {
			s.config.ExternalDNS.CertDir = *update.CertDir
		}

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Settings updated")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// Helper functions for external DNS

func (s *Server) getPublicIP() (string, error) {
	// Use configured public IP if set
	if s.config.ExternalDNS != nil && s.config.ExternalDNS.PublicIP != "" {
		return s.config.ExternalDNS.PublicIP, nil
	}
	return s.detectPublicIP()
}

func (s *Server) detectPublicIP() (string, error) {
	return dns.GetPublicIP()
}

func (s *Server) findZone(id string) *config.ExternalDNSZone {
	if s.config.ExternalDNS == nil {
		return nil
	}
	for i := range s.config.ExternalDNS.Zones {
		if s.config.ExternalDNS.Zones[i].ID == id {
			return &s.config.ExternalDNS.Zones[i]
		}
	}
	return nil
}

func (s *Server) updateZone(id string, update config.ExternalDNSZone) error {
	if s.config.ExternalDNS == nil {
		return fmt.Errorf("external DNS not configured")
	}
	for i := range s.config.ExternalDNS.Zones {
		if s.config.ExternalDNS.Zones[i].ID == id {
			update.ID = id // preserve ID
			s.config.ExternalDNS.Zones[i] = update
			return s.saveConfig()
		}
	}
	return fmt.Errorf("zone not found")
}

func (s *Server) deleteZone(id string) error {
	if s.config.ExternalDNS == nil {
		return fmt.Errorf("external DNS not configured")
	}
	for i := range s.config.ExternalDNS.Zones {
		if s.config.ExternalDNS.Zones[i].ID == id {
			s.config.ExternalDNS.Zones = append(s.config.ExternalDNS.Zones[:i], s.config.ExternalDNS.Zones[i+1:]...)
			return s.saveConfig()
		}
	}
	return fmt.Errorf("zone not found")
}

func (s *Server) findRecord(id string) *config.ExternalDNSRecord {
	if s.config.ExternalDNS == nil {
		return nil
	}
	for i := range s.config.ExternalDNS.Records {
		if s.config.ExternalDNS.Records[i].ID == id {
			return &s.config.ExternalDNS.Records[i]
		}
	}
	return nil
}

func (s *Server) updateRecord(id string, update config.ExternalDNSRecord) error {
	if s.config.ExternalDNS == nil {
		return fmt.Errorf("external DNS not configured")
	}
	for i := range s.config.ExternalDNS.Records {
		if s.config.ExternalDNS.Records[i].ID == id {
			update.ID = id // preserve ID
			s.config.ExternalDNS.Records[i] = update
			return s.saveConfig()
		}
	}
	return fmt.Errorf("record not found")
}

func (s *Server) deleteRecord(id string) error {
	if s.config.ExternalDNS == nil {
		return fmt.Errorf("external DNS not configured")
	}
	for i := range s.config.ExternalDNS.Records {
		if s.config.ExternalDNS.Records[i].ID == id {
			s.config.ExternalDNS.Records = append(s.config.ExternalDNS.Records[:i], s.config.ExternalDNS.Records[i+1:]...)
			return s.saveConfig()
		}
	}
	return fmt.Errorf("record not found")
}

func (s *Server) syncDNSRecords(ctx context.Context, recordID string, dryRun bool) ([]map[string]interface{}, error) {
	if s.config.ExternalDNS == nil {
		return nil, fmt.Errorf("external DNS not configured")
	}

	modeStr := ""
	if dryRun {
		modeStr = " (DRY RUN)"
	}

	recordCount := len(s.config.ExternalDNS.Records)
	if recordID != "" {
		recordCount = 1
	}
	logger.Info("DNS Sync: Starting sync for %d record(s)%s", recordCount, modeStr)

	publicIP, err := s.getPublicIP()
	if err != nil {
		logger.Error("DNS Sync: Failed to get public IP: %v", err)
		return nil, fmt.Errorf("failed to get public IP: %w", err)
	}
	logger.Info("DNS Sync: Current public IP is %s", publicIP)

	var results []map[string]interface{}
	var changedCount, unchangedCount, errorCount int

	for i := range s.config.ExternalDNS.Records {
		rec := &s.config.ExternalDNS.Records[i]

		// Skip if syncing specific record and this isn't it
		if recordID != "" && rec.ID != recordID {
			continue
		}

		// Find zone for this record
		zone := s.findZone(rec.ZoneID)
		if zone == nil {
			logger.Error("DNS Sync: Zone not found for record %s", rec.Name)
			results = append(results, map[string]interface{}{
				"record_id": rec.ID,
				"name":      rec.Name,
				"error":     "zone not found",
			})
			errorCount++
			continue
		}

		logger.Info("DNS Sync: Processing %s %s in zone %s", rec.Type, rec.Name, zone.Name)

		// Create provider
		provider, err := dns.NewProvider(&zone.Provider, zone.Name)
		if err != nil {
			logger.Error("DNS Sync: Failed to create provider for %s: %v", rec.Name, err)
			results = append(results, map[string]interface{}{
				"record_id": rec.ID,
				"name":      rec.Name,
				"error":     err.Error(),
			})
			errorCount++
			continue
		}

		// Use public IP for A records if value is empty or auto_sync is enabled
		value := rec.Value
		if rec.Type == "A" && (value == "" || rec.AutoSync) {
			value = publicIP
			logger.Info("DNS Sync: Using public IP %s for %s (auto-sync enabled)", value, rec.Name)
		}

		// Sync the record
		dnsRec := dns.Record{
			Name:   rec.Name,
			Type:   rec.Type,
			Value:  value,
			TTL:    rec.TTL,
			ZoneID: zone.ID,
		}

		if dryRun {
			// In dry-run mode, just log what would happen
			logger.Info("DNS Sync: Would update %s %s -> %s (TTL: %d)", rec.Type, rec.Name, value, rec.TTL)
			results = append(results, map[string]interface{}{
				"record_id": rec.ID,
				"name":      rec.Name,
				"value":     value,
				"changed":   false,
				"dry_run":   true,
				"success":   true,
			})
			unchangedCount++
		} else {
			changed, err := provider.SyncRecord(zone.ID, dnsRec)
			if err != nil {
				logger.Error("DNS Sync: Failed to sync %s: %v", rec.Name, err)
				results = append(results, map[string]interface{}{
					"record_id": rec.ID,
					"name":      rec.Name,
					"error":     err.Error(),
				})
				errorCount++
				continue
			}

			if changed {
				logger.Info("DNS Sync: Updated %s %s -> %s", rec.Type, rec.Name, value)
				changedCount++
			} else {
				logger.Info("DNS Sync: No change needed for %s (already %s)", rec.Name, value)
				unchangedCount++
			}

			results = append(results, map[string]interface{}{
				"record_id": rec.ID,
				"name":      rec.Name,
				"value":     value,
				"changed":   changed,
				"success":   true,
			})
		}
	}

	// Update last synced timestamp for zones (only if not dry-run)
	if !dryRun {
		now := time.Now().Format(time.RFC3339)
		for i := range s.config.ExternalDNS.Zones {
			s.config.ExternalDNS.Zones[i].LastSynced = now
		}
		s.saveConfig()
	}

	logger.Info("DNS Sync: Completed%s - %d changed, %d unchanged, %d errors", modeStr, changedCount, unchangedCount, errorCount)

	return results, nil
}

// startDynamicDNSSync starts a background goroutine that periodically checks
// the public IP and syncs DNS records if the IP has changed.
func (s *Server) startDynamicDNSSync(ctx context.Context) {
	go func() {
		// Initial sync on startup
		s.checkAndSyncDynamicDNS(ctx)

		interval := time.Duration(s.getSyncInterval()) * time.Minute
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("[DynDNS] Stopping dynamic DNS sync")
				return
			case <-ticker.C:
				// Check if config changed the interval
				newInterval := time.Duration(s.getSyncInterval()) * time.Minute
				if newInterval != interval {
					ticker.Reset(newInterval)
					interval = newInterval
					log.Printf("[DynDNS] Sync interval changed to %v", interval)
				}
				s.checkAndSyncDynamicDNS(ctx)
			}
		}
	}()
}

// checkAndSyncDynamicDNS checks if the public IP changed and syncs auto_sync records if needed.
func (s *Server) checkAndSyncDynamicDNS(ctx context.Context) {
	s.mu.RLock()
	extDNS := s.config.ExternalDNS
	s.mu.RUnlock()

	// Skip if external DNS is not configured or not enabled
	if extDNS == nil || !extDNS.Enabled || !extDNS.AutoSyncIP {
		return
	}

	// Get current public IP
	publicIP, err := s.getPublicIP()
	if err != nil {
		log.Printf("[DynDNS] Failed to get public IP: %v", err)
		return
	}

	// Check if IP changed
	s.mu.Lock()
	lastIP := s.lastPublicIP
	if publicIP == lastIP {
		s.mu.Unlock()
		return
	}

	// IP changed, update our tracking
	s.lastPublicIP = publicIP
	s.mu.Unlock()

	if lastIP == "" {
		log.Printf("[DynDNS] Initial public IP: %s", publicIP)
	} else {
		log.Printf("[DynDNS] Public IP changed: %s -> %s", lastIP, publicIP)
	}

	// Sync only auto_sync records
	s.mu.RLock()
	records := s.config.ExternalDNS.Records
	s.mu.RUnlock()

	syncedCount := 0
	for _, rec := range records {
		if !rec.AutoSync || rec.Type != "A" {
			continue
		}

		// Find zone for this record
		zone := s.findZone(rec.ZoneID)
		if zone == nil {
			log.Printf("[DynDNS] Zone not found for record %s", rec.Name)
			continue
		}

		// Create provider
		provider, err := dns.NewProvider(&zone.Provider, zone.Name)
		if err != nil {
			log.Printf("[DynDNS] Failed to create provider for %s: %v", rec.Name, err)
			continue
		}

		// Sync the record
		dnsRec := dns.Record{
			Name:   rec.Name,
			Type:   rec.Type,
			Value:  publicIP,
			TTL:    rec.TTL,
			ZoneID: zone.ID,
		}

		changed, err := provider.SyncRecord(zone.ID, dnsRec)
		if err != nil {
			log.Printf("[DynDNS] Failed to sync %s: %v", rec.Name, err)
			continue
		}

		if changed {
			log.Printf("[DynDNS] Updated %s -> %s", rec.Name, publicIP)
			syncedCount++
		}
	}

	if syncedCount > 0 {
		// Update last synced timestamp
		s.mu.Lock()
		now := time.Now().Format(time.RFC3339)
		for i := range s.config.ExternalDNS.Zones {
			s.config.ExternalDNS.Zones[i].LastSynced = now
		}
		s.saveConfig()
		s.mu.Unlock()
	}
}

// =============================================================================
// Service Management API handlers
// =============================================================================

// /api/service/status - GET systemd service status
func (s *Server) handleAPIServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	status := systemd.GetStatus(s.configPath)
	writeJSON(w, status)
}

// /api/service/install - POST install/update systemd service
func (s *Server) handleAPIServiceInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.dryRun {
		writeError(w, http.StatusBadRequest, "Cannot install service in dry-run mode")
		return
	}

	if err := systemd.Install(s.configPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Systemd service installed/updated")
	writeSuccess(w, "Service installed successfully. Run 'systemctl daemon-reload' if needed.")
}

// /api/service/enable - POST enable systemd service
func (s *Server) handleAPIServiceEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.dryRun {
		writeError(w, http.StatusBadRequest, "Cannot enable service in dry-run mode")
		return
	}

	if err := systemd.Enable(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Systemd service enabled")
	writeSuccess(w, "Service enabled for auto-start")
}

// /api/service/start - POST start systemd service
func (s *Server) handleAPIServiceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.dryRun {
		writeError(w, http.StatusBadRequest, "Cannot start service in dry-run mode")
		return
	}

	if err := systemd.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Systemd service started")
	writeSuccess(w, "Service started")
}

// /api/service/restart - POST restart systemd service
func (s *Server) handleAPIServiceRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.dryRun {
		writeError(w, http.StatusBadRequest, "Cannot restart service in dry-run mode")
		return
	}

	if err := systemd.Restart(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Info("Systemd service restarted")
	writeSuccess(w, "Service restarted")
}

// /api/stats/interfaces - GET interface statistics and rates
func (s *Server) handleAPIStatsInterfaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if a specific interface was requested
	ifaceName := r.URL.Query().Get("interface")

	if ifaceName != "" {
		rates, err := s.stats.GetRates(ctx, ifaceName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, rates)
		return
	}

	// Return all interfaces
	rates, err := s.stats.GetAllRates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rates)
}

// /api/stats/latency - GET/POST latency measurements
func (s *Server) handleAPIStatsLatency(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method == http.MethodGet {
		// Return latency history
		history := s.stats.GetLatencyHistory()

		// If history is empty, take a measurement now
		if len(history) == 0 {
			latency, err := s.stats.MeasureLatency(ctx)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, map[string]interface{}{
				"current": latency,
				"history": []interface{}{latency},
			})
			return
		}

		writeJSON(w, map[string]interface{}{
			"current": history[len(history)-1],
			"history": history,
		})
		return
	}

	if r.Method == http.MethodPost {
		// Configure latency target or trigger a measurement
		var req struct {
			Target  string `json:"target"`
			Measure bool   `json:"measure"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Target != "" {
			s.stats.SetLatencyTarget(req.Target)
		}

		if req.Measure {
			latency, err := s.stats.MeasureLatency(ctx)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, latency)
			return
		}

		writeSuccess(w, "Latency target updated")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/stats/snapshot - GET complete stats snapshot
func (s *Server) handleAPIStatsSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	snapshot, err := s.stats.GetSnapshot(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, snapshot)
}

// /api/stats/wifi/clients - GET WiFi client statistics with bandwidth rates
func (s *Server) handleAPIStatsWiFiClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	clients, err := s.stats.GetWiFiClientRates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"clients": clients,
	})
}

// /api/stats/history - GET historical stats data
func (s *Server) handleAPIStatsHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query parameters
	ifaceName := r.URL.Query().Get("interface")
	if ifaceName == "" {
		writeError(w, http.StatusBadRequest, "interface parameter required")
		return
	}

	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "24h" // Default to 24 hours
	}

	start, end, resolution, err := stats.ParseRange(rangeStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Allow explicit resolution override
	if resolutionStr := r.URL.Query().Get("resolution"); resolutionStr != "" {
		resolution = stats.Resolution(resolutionStr)
	}

	query := stats.HistoryQuery{
		InterfaceName: ifaceName,
		Resolution:    resolution,
		Start:         start,
		End:           end,
	}

	response, err := s.stats.QueryHistory(ctx, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if response == nil {
		writeError(w, http.StatusServiceUnavailable, "Stats persistence not configured")
		return
	}

	writeJSON(w, response)
}

// /api/stats/wifi-clients/history - GET historical WiFi client stats
func (s *Server) handleAPIStatsWiFiClientHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query parameters
	mac := r.URL.Query().Get("mac") // Optional - empty for all clients

	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "24h" // Default to 24 hours
	}

	start, end, resolution, err := stats.ParseRange(rangeStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Allow explicit resolution override
	if resolutionStr := r.URL.Query().Get("resolution"); resolutionStr != "" {
		resolution = stats.Resolution(resolutionStr)
	}

	query := stats.WiFiClientHistoryQuery{
		MAC:        mac,
		Resolution: resolution,
		Start:      start,
		End:        end,
	}

	response, err := s.stats.QueryWiFiClientHistory(ctx, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if response == nil {
		writeError(w, http.StatusServiceUnavailable, "Stats persistence not configured")
		return
	}

	writeJSON(w, response)
}

// /api/qos/status - GET QoS status
func (s *Server) handleAPIQoSStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	status, err := s.qos.GetStatus(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status)
}

// /api/qos/configure - POST configure QoS
func (s *Server) handleAPIQoSConfigure(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Enabled         bool   `json:"enabled"`
		Mode            string `json:"mode"`
		DownloadSpeed   string `json:"download_speed"`
		UploadSpeed     string `json:"upload_speed"`
		PerHostFairness bool   `json:"per_host_fairness"`
		RTTCompensation string `json:"rtt_compensation"`
		WANType         string `json:"wan_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build QoS config
	cfg := &qos.Config{
		Enabled:         req.Enabled,
		Mode:            qos.Mode(req.Mode),
		WANInterface:    s.config.WANInterface,
		LANInterface:    s.config.LANBridge,
		DownloadSpeed:   req.DownloadSpeed,
		UploadSpeed:     req.UploadSpeed,
		PerHostFairness: req.PerHostFairness,
		RTTCompensation: req.RTTCompensation,
		WANType:         req.WANType,
	}

	// Apply QoS
	if err := s.qos.Configure(ctx, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Save to config
	s.mu.Lock()
	if s.config.QoS == nil {
		s.config.QoS = &config.QoSConfig{}
	}
	s.config.QoS.Enabled = req.Enabled
	s.config.QoS.Mode = req.Mode
	s.config.QoS.DownloadSpeed = req.DownloadSpeed
	s.config.QoS.UploadSpeed = req.UploadSpeed
	s.config.QoS.PerHostFairness = req.PerHostFairness
	s.config.QoS.RTTCompensation = req.RTTCompensation
	s.config.QoS.WANType = req.WANType
	s.mu.Unlock()

	if err := s.saveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	logger.Info("QoS configured: enabled=%v mode=%s download=%s upload=%s",
		req.Enabled, req.Mode, req.DownloadSpeed, req.UploadSpeed)

	writeSuccess(w, "QoS configured successfully")
}

// =============================================================================
// Device Management API handlers
// =============================================================================

// /api/devices - GET all devices, POST new device
func (s *Server) handleAPIDevices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		devices, err := s.statsStore.GetAllDevices(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if devices == nil {
			devices = []stats.Device{}
		}
		writeJSON(w, map[string]interface{}{
			"devices": devices,
		})

	case http.MethodPost:
		var device stats.Device
		if err := json.NewDecoder(r.Body).Decode(&device); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if device.MAC == "" {
			writeError(w, http.StatusBadRequest, "MAC address is required")
			return
		}

		// Set timestamps if not provided
		now := time.Now().Unix()
		if device.FirstSeen == 0 {
			device.FirstSeen = now
		}
		device.LastSeen = now

		if err := s.statsStore.UpsertDevice(ctx, device); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"device":  device,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/devices/{mac} - GET/PUT/DELETE specific device
func (s *Server) handleAPIDevice(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Extract MAC from URL path
	mac := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	mac = strings.TrimSuffix(mac, "/")
	if mac == "" {
		writeError(w, http.StatusBadRequest, "MAC address required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		device, err := s.statsStore.GetDevice(ctx, mac)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if device == nil {
			writeError(w, http.StatusNotFound, "Device not found")
			return
		}
		writeJSON(w, device)

	case http.MethodPut:
		var update stats.Device
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Ensure MAC matches URL
		update.MAC = mac
		update.LastSeen = time.Now().Unix()

		// Get existing device to preserve first_seen
		existing, _ := s.statsStore.GetDevice(ctx, mac)
		if existing != nil && update.FirstSeen == 0 {
			update.FirstSeen = existing.FirstSeen
		}
		if update.FirstSeen == 0 {
			update.FirstSeen = update.LastSeen
		}

		if err := s.statsStore.UpsertDevice(ctx, update); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"device":  update,
		})

	case http.MethodDelete:
		if err := s.statsStore.DeleteDevice(ctx, mac); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, "Device deleted")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// =============================================================================
// Notification API handlers
// =============================================================================

// /api/notifications/settings - GET/POST notification settings
func (s *Server) handleAPINotificationsSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config := s.notifier.GetConfig()
		writeJSON(w, map[string]interface{}{
			"enabled":              config.Enabled,
			"server":               config.Server,
			"topic":                config.Topic,
			"has_token":            config.Token != "",
			"has_auth":             config.Username != "" && config.Password != "",
			"notify_wifi_clients":  config.NotifyWiFiClients,
			"notify_health_checks": config.NotifyHealthChecks,
			"notify_wan_changes":   config.NotifyWANChanges,
			"notify_vpn_peers":     config.NotifyVPNPeers,
			"notify_system_events": config.NotifySystemEvents,
		})

	case http.MethodPost:
		var req struct {
			Enabled            bool   `json:"enabled"`
			Server             string `json:"server"`
			Topic              string `json:"topic"`
			Token              string `json:"token"`
			Username           string `json:"username"`
			Password           string `json:"password"`
			NotifyWiFiClients  bool   `json:"notify_wifi_clients"`
			NotifyHealthChecks bool   `json:"notify_health_checks"`
			NotifyWANChanges   bool   `json:"notify_wan_changes"`
			NotifyVPNPeers     bool   `json:"notify_vpn_peers"`
			NotifySystemEvents bool   `json:"notify_system_events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Update notifier config
		ntfyConfig := notifications.Config{
			Enabled:            req.Enabled,
			Server:             req.Server,
			Topic:              req.Topic,
			Token:              req.Token,
			Username:           req.Username,
			Password:           req.Password,
			NotifyWiFiClients:  req.NotifyWiFiClients,
			NotifyHealthChecks: req.NotifyHealthChecks,
			NotifyWANChanges:   req.NotifyWANChanges,
			NotifyVPNPeers:     req.NotifyVPNPeers,
			NotifySystemEvents: req.NotifySystemEvents,
		}

		// Default server if not provided
		if ntfyConfig.Server == "" {
			ntfyConfig.Server = "https://ntfy.sh"
		}

		s.notifier.SetConfig(ntfyConfig)

		// Save to config file
		s.mu.Lock()
		if s.config.Notifications == nil {
			s.config.Notifications = &config.NotificationsConfig{}
		}
		s.config.Notifications.Enabled = req.Enabled
		s.config.Notifications.Server = ntfyConfig.Server
		s.config.Notifications.Topic = req.Topic
		s.config.Notifications.Token = req.Token
		s.config.Notifications.Username = req.Username
		s.config.Notifications.Password = req.Password
		s.config.Notifications.NotifyWiFiClients = req.NotifyWiFiClients
		s.config.Notifications.NotifyHealthChecks = req.NotifyHealthChecks
		s.config.Notifications.NotifyWANChanges = req.NotifyWANChanges
		s.config.Notifications.NotifyVPNPeers = req.NotifyVPNPeers
		s.config.Notifications.NotifySystemEvents = req.NotifySystemEvents
		s.mu.Unlock()

		if err := s.saveConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
			return
		}

		logger.Info("Notification settings updated: enabled=%v server=%s topic=%s", req.Enabled, ntfyConfig.Server, req.Topic)
		writeSuccess(w, "Notification settings updated")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// /api/notifications/test - POST send test notification
func (s *Server) handleAPINotificationsTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Send a test notification
	err := s.notifier.Send(ctx, "test", "Test Notification", "This is a test notification from Ubuntu Router.", notifications.PriorityDefault, "test_tube")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to send test notification: "+err.Error())
		return
	}

	writeSuccess(w, "Test notification sent")
}

// /api/notifications/events - GET notification event log
func (s *Server) handleAPINotificationsEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := s.ctx(r)
	defer cancel()

	// Parse limit parameter
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	events, err := s.statsStore.GetNotificationEvents(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if events == nil {
		events = []stats.NotificationEvent{}
	}

	writeJSON(w, map[string]interface{}{
		"events": events,
	})
}

// =============================================================================
// Helper functions for SQLite-based config data
// =============================================================================

// regenerateDNSHosts reads DNS entries from SQLite and regenerates the hosts file
func (s *Server) regenerateDNSHosts(ctx context.Context) error {
	entries, err := s.statsStore.GetDNSEntries(ctx)
	if err != nil {
		return err
	}

	// Convert stats.DNSEntry to config.DNSEntry for dnsmasq
	configEntries := make([]config.DNSEntry, len(entries))
	for i, e := range entries {
		configEntries[i] = config.DNSEntry{
			Hostname: e.Hostname,
			IP:       e.IP,
		}
	}

	// Write hosts file
	if err := s.dns.WriteHosts(configEntries); err != nil {
		return err
	}

	// Reload dnsmasq
	return s.dns.Reload(ctx)
}

// regenerateDHCPConfig reads DHCP reservations from SQLite and regenerates the config
func (s *Server) regenerateDHCPConfig(ctx context.Context) error {
	reservations, err := s.statsStore.GetDHCPReservations(ctx)
	if err != nil {
		return err
	}

	// Convert stats.DHCPReservation to config.DHCPReservation
	configReservations := make([]config.DHCPReservation, len(reservations))
	for i, r := range reservations {
		configReservations[i] = config.DHCPReservation{
			Name: r.Name,
			MAC:  r.MAC,
			IP:   r.IP,
		}
	}

	// Temporarily set reservations in config for dnsmasq to write
	oldReservations := s.config.DHCPReservations
	s.config.DHCPReservations = configReservations

	// Write DHCP config
	err = s.dns.WriteDHCPConfig(s.config)

	// Restore original config (we don't store in JSON anymore)
	s.config.DHCPReservations = oldReservations

	if err != nil {
		return err
	}

	// Reload dnsmasq
	return s.dns.Reload(ctx)
}

// ============================================================================
// Services API Handlers
// ============================================================================

// /api/services/status - GET services status
func (s *Server) handleAPIServicesStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.svcManager == nil {
		writeJSON(w, map[string]interface{}{
			"enabled":       false,
			"service_count": 0,
			"active_count":  0,
			"haproxy_ok":    false,
		})
		return
	}

	status, err := s.svcManager.GetStatus(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, status)
}

// /api/services/settings - GET/POST services settings
func (s *Server) handleAPIServicesSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		enabled := false
		if s.config.Services != nil {
			enabled = s.config.Services.Enabled
		}
		writeJSON(w, map[string]interface{}{
			"enabled": enabled,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if s.svcManager == nil {
			writeError(w, http.StatusInternalServerError, "Services manager not initialized")
			return
		}

		if err := s.svcManager.UpdateSettings(req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "Settings updated")
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/services - GET/POST services
func (s *Server) handleAPIServices(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var services []config.Service
		if s.config.Services != nil {
			services = s.config.Services.Services
		}
		if services == nil {
			services = []config.Service{}
		}
		writeJSON(w, map[string]interface{}{
			"services": services,
		})
		return
	}

	if r.Method == http.MethodPost {
		if s.svcManager == nil {
			writeError(w, http.StatusInternalServerError, "Services manager not initialized")
			return
		}

		var svc config.Service
		if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		created, err := s.svcManager.CreateService(svc)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"service": created,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// /api/services/{id} - GET/PUT/DELETE service
func (s *Server) handleAPIService(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	// Extract service ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/services/")
	parts := strings.Split(path, "/")
	serviceID := parts[0]

	// Handle /api/services/{id}/sync
	if len(parts) > 1 && parts[1] == "sync" {
		s.handleAPIServiceSync(w, r, serviceID)
		return
	}

	if serviceID == "" {
		writeError(w, http.StatusBadRequest, "service ID required")
		return
	}

	if s.svcManager == nil {
		writeError(w, http.StatusInternalServerError, "Services manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		svc := s.svcManager.GetService(serviceID)
		if svc == nil {
			writeError(w, http.StatusNotFound, "service not found")
			return
		}
		writeJSON(w, svc)

	case http.MethodPut:
		var update config.Service
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		updated, err := s.svcManager.UpdateService(serviceID, update)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"success": true,
			"service": updated,
		})

	case http.MethodDelete:
		if err := s.svcManager.DeleteService(ctx, serviceID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, "Service deleted")

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIServiceSync syncs a single service
func (s *Server) handleAPIServiceSync(w http.ResponseWriter, r *http.Request, serviceID string) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.svcManager == nil {
		writeError(w, http.StatusInternalServerError, "Services manager not initialized")
		return
	}

	result, err := s.svcManager.SyncService(ctx, serviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, result)
}

// /api/services/sync-all - POST sync all services
func (s *Server) handleAPIServicesSyncAll(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.svcManager == nil {
		writeError(w, http.StatusInternalServerError, "Services manager not initialized")
		return
	}

	results, err := s.svcManager.SyncAll(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"success": true,
		"results": results,
	})
}

// ============================================================================
// HAProxy API Handlers
// ============================================================================

// /api/haproxy/status - GET HAProxy status
func (s *Server) handleAPIHAProxyStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.haproxy == nil {
		writeError(w, http.StatusInternalServerError, "HAProxy manager not initialized")
		return
	}

	var services []config.Service
	if s.config.Services != nil {
		services = s.config.Services.Services
	}

	status, err := s.haproxy.GetStatus(ctx, services)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, status)
}

// /api/haproxy/reload - POST reload HAProxy
func (s *Server) handleAPIHAProxyReload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.haproxy == nil {
		writeError(w, http.StatusInternalServerError, "HAProxy manager not initialized")
		return
	}

	if err := s.haproxy.Reload(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "HAProxy reloaded")
}

// /api/haproxy/config - GET HAProxy config
func (s *Server) handleAPIHAProxyConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.haproxy == nil {
		writeError(w, http.StatusInternalServerError, "HAProxy manager not initialized")
		return
	}

	var services []config.Service
	var zones []config.ExternalDNSZone
	if s.config.Services != nil {
		services = s.config.Services.Services
	}
	if s.config.ExternalDNS != nil {
		zones = s.config.ExternalDNS.Zones
	}

	configStr := s.haproxy.GenerateConfig(services, zones, s.config.LANAddresses)

	writeJSON(w, map[string]interface{}{
		"config": configStr,
	})
}

// /api/haproxy/test - POST test HAProxy config
func (s *Server) handleAPIHAProxyTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.haproxy == nil {
		writeError(w, http.StatusInternalServerError, "HAProxy manager not initialized")
		return
	}

	if err := s.haproxy.TestConfig(ctx); err != nil {
		writeJSON(w, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, map[string]interface{}{
		"valid": true,
	})
}

// /api/haproxy/backends - GET backend statuses
func (s *Server) handleAPIHAProxyBackends(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.haproxy == nil {
		writeError(w, http.StatusInternalServerError, "HAProxy manager not initialized")
		return
	}

	var services []config.Service
	var zones []config.ExternalDNSZone
	if s.config.Services != nil {
		services = s.config.Services.Services
	}
	if s.config.ExternalDNS != nil {
		zones = s.config.ExternalDNS.Zones
	}

	statuses := s.haproxy.GetBackendStatuses(ctx, services, zones)

	writeJSON(w, map[string]interface{}{
		"backends": statuses,
	})
}
