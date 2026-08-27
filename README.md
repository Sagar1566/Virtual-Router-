# Ubuntu Router

Turn any Ubuntu/Linux machine into a full-featured network router with a modern web interface.

## Features

- **Multi-WAN with Failover** - Multiple internet connections with automatic failover and health monitoring
- **WiFi Access Point** - Create 2.4GHz/5GHz access points with hostapd, monitor connected clients
- **WiFi Client (WAN)** - Use WiFi as a WAN uplink for portable router setups
- **DHCP & DNS Server** - dnsmasq-based with static leases, reservations, and local DNS zones
- **WireGuard VPN** - Both client-server VPN and site-to-site P2P tunnels with QR code provisioning
- **Firewall & NAT** - iptables-based with port forwarding and custom rules
- **External DNS** - Integration with Route53, Cloudflare, DigitalOcean for dynamic DNS and Let's Encrypt SSL
- **Configuration Rollback** - Automatic rollback if you lose connectivity after network changes
- **Device Tracking** - Track devices by MAC address with hostname detection from DHCP
- **Real-time Logs** - WebSocket-based log streaming in the web UI

## Screenshots

The web interface provides a clean dashboard with system status, connected clients, and quick access to all features.

## Requirements

- Ubuntu 22.04+ or Debian 12+ (arm64 or amd64)
- Root access
- Network interfaces (Ethernet, WiFi adapters)

### System Dependencies

```bash
# Required
sudo apt install dnsmasq iptables iproute2

# For WiFi AP
sudo apt install hostapd

# For WiFi Client WAN
sudo apt install wpasupplicant

# For WireGuard VPN
sudo apt install wireguard wireguard-tools

# For DHCP client (failover)
sudo apt install dhcpcd
```

## Quick Start (Recommended - All-in-One Script)

The fastest and easiest way to run the project is using the automated runner script [`./start.sh`](start.sh). It automatically checks, downloads, and sets up all missing dependencies (Go compiler, Node.js, npm), builds the React frontend and Go backend, and launches the server in a single command.

```bash
# Make script executable (if needed) and run
chmod +x start.sh
./start.sh
```

### Runner Script Options

| Command | Description |
|---|---|
| `./start.sh` | Run in safe **Dry-Run mode** (Default, no root required) |
| `sudo ./start.sh --live` | Run in **Live mode** with real networking changes (Requires root) |
| `./start.sh --rebuild` | Force a clean rebuild of the web frontend and backend binary |
| `./start.sh --port 9090` | Launch the web interface on a custom HTTP port |

---

### Accessing the Web UI

After running `./start.sh`, open **`http://localhost:8080`** (or `http://<router-ip>:8080`) in your browser.
* The admin password is displayed on the console at startup and saved in `config.json.password`.

---

### Alternative: Manual Download & Binary Installation

```bash
# 1. Download the latest release for your architecture
wget https://github.com/iodesystems/ubuntu-router/releases/latest/download/ubuntu-router-linux-amd64
chmod +x ubuntu-router-linux-amd64
sudo mv ubuntu-router-linux-amd64 /usr/local/bin/ubuntu-router

# 2. Run manually
sudo ubuntu-router
```

### Install as Systemd Service

From the web UI, go to **Settings > Service** and click "Install Service" to set up systemd auto-start on boot.

## Configuration

Config is stored in JSON format. Default locations (searched in order):
1. `./config.json`
2. `./ubuntu-router.json`
3. `/etc/ubuntu-router/config.json`
4. `/etc/ubuntu-router.json`

The admin password is stored separately in `<config-path>.password` and auto-generated on first run.

### Example Config

```json
{
  "listen_addr": ":8080",

  "wans": [
    {
      "name": "Primary",
      "interface": "eth0",
      "enabled": true,
      "priority": 1,
      "mode": "dhcp",
      "health_check_interval": 10,
      "health_check_targets": ["8.8.8.8", "1.1.1.1"]
    },
    {
      "name": "Backup WiFi",
      "interface": "wlan1",
      "enabled": true,
      "priority": 2,
      "mode": "wifi",
      "wifi_ssid": "MyNetwork",
      "wifi_password": "secret",
      "health_check_interval": 10
    }
  ],

  "lan_bridge": "br0",
  "lan_addresses": ["192.168.2.1/24"],
  "lan_ports": ["eth1", "eth2"],

  "dhcp_enabled": true,
  "dhcp_start": "192.168.2.100",
  "dhcp_end": "192.168.2.200",
  "dhcp_lease": "24h",

  "dns_enabled": true,
  "dns_upstream": ["8.8.8.8", "1.1.1.1"],

  "wifi_interfaces": [
    {
      "interface": "wlan0",
      "enabled": true,
      "ssid": "MyRouter",
      "password": "secretpassword",
      "channel": 6,
      "band": "2.4ghz",
      "bridge": "br0"
    }
  ],

  "firewall_enabled": true,
  "port_forwards": [
    {"name": "SSH", "proto": "tcp", "port": 22, "dest_ip": "192.168.2.10"}
  ]
}
```

## Multi-WAN Failover

Ubuntu Router supports multiple WAN connections with automatic failover:

- **Priority-based** - Lower priority number = preferred connection
- **Health Checks** - Ping gateway and external targets to detect failures
- **Automatic Failover** - Switches to backup WAN when primary fails
- **Failback Delay** - Configurable delay before switching back to primary
- **Per-WAN DHCP** - Each WAN maintains its own DHCP lease for fast failover

Supported WAN modes:
- `dhcp` - Automatic IP via DHCP
- `static` - Manual IP configuration
- `wifi` - WiFi client mode (connect to upstream WiFi network)
- `pppoe` - PPPoE for DSL connections

## Building from Source

```bash
# Clone
git clone https://github.com/iodesystems/ubuntu-router.git
cd ubuntu-router

# Build
make build          # Build Go binary
make build-web      # Build React frontend

# Or build everything
make all

# Cross-compile for deployment
make dist           # Creates binaries for arm64, armv7, amd64
```

### Development

```bash
# Run in dry-run mode (no system changes)
make dev

# Run frontend dev server with hot reload
cd web && npm run dev

# Run tests
make test

# Lint
make lint
```

### Deploy to Target

```bash
# Deploy to a remote system
./deploy.sh ubuntu@192.168.1.1
```

## API

All endpoints are under `/api/` and require authentication via session cookie.

See [CLAUDE.md](CLAUDE.md) for full API documentation.

## License

MIT License - See [LICENSE](LICENSE) for details.

## Contributing

Contributions welcome! Please read the development section and ensure tests pass before submitting PRs.
