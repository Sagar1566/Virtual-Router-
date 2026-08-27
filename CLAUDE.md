# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Ubuntu Router is a network router management system that turns Ubuntu/Linux into a full-featured router with web-based administration. It manages WAN, LAN, DHCP, DNS, WiFi (hostapd), WireGuard VPN, and iptables firewall through a unified interface.

**Key Features:**
- **WAN Management** - DHCP, static IP, PPPoE, or WiFi client modes with multi-WAN failover
- **LAN Management** - Bridge configuration, multiple LAN addresses, port aggregation
- **DHCP/DNS Server** - dnsmasq-based with static leases, reservations, and local DNS zones
- **WiFi Access Point** - hostapd-based AP with 2.4GHz/5GHz support, station monitoring
- **WireGuard VPN** - Both client-server VPN and site-to-site P2P tunnels
- **Firewall** - iptables-based with port forwarding and custom rules
- **External DNS** - Integration with Route53, Cloudflare, DigitalOcean, etc. with Let's Encrypt SSL
- **Configuration Rollback** - Commit-confirm semantics with automatic rollback if connectivity lost

## Build Commands

```bash
make build          # Build Go binary
make build-web      # Build React/TypeScript frontend (runs npm install && npm run build)
make run            # Build and run with sudo (makes actual system changes)
make dev            # Build and run in dry-run mode (no system changes)
make test           # Run Go tests
make lint           # Run golangci-lint
make fmt            # Format Go code
make check          # Validate system dependencies (dnsmasq, wireguard, hostapd, iptables)
make install        # Install binary to /usr/local/bin and set up systemd service
make dist           # Cross-compile for arm64, armv7, amd64
```

**VM Testing (requires Multipass):**
```bash
make vm-create      # Create Ubuntu 24.04 VM with dependencies
make vm-deploy      # Build and deploy to VM
make vm-run         # Run in VM (dry-run mode)
make vm-run-live    # Run in VM (live mode)
make vm-shell       # Open shell in VM
make vm-destroy     # Delete the VM
```

## Architecture

**Backend (Go 1.22):** Single statically-compiled binary in `cmd/ubuntu-router/main.go`

**Internal packages** (`internal/`):
- `config/` - JSON config parsing with JSONC support, admin password management. Main config struct defines all router settings
- `server/` - HTTP server, REST API handlers (`api.go`), session auth (HMAC-SHA256 cookies)
- `network/` - Interface enumeration, bridge management, IP configuration using `ip` command
- `dnsmasq/` - DNS/DHCP config generation (`/etc/dnsmasq.d/`), lease parsing from `/var/lib/misc/dnsmasq.leases`
- `wifi/` - hostapd configuration and station management via `hostapd_cli`
- `wireguard/` - Peer management, key generation, QR codes, P2P site-to-site tunnels
- `system/` - `FileSystem` and `CommandRunner` interfaces (enables dry-run mode and testing)
- `rollback/` - Configuration rollback with commit-confirm semantics (30s timeout)
- `health/` - Modular health check system with dependency ordering and auto-fixers
- `multiwan/` - Multi-WAN failover with health checks (ping-based)
- `dns/` - External DNS provider integration via libdns
- `acme/` - Let's Encrypt certificate management

**Frontend (React 19 + TypeScript)** in `web/`:
- Vite build, Material-UI, TanStack Router
- API client in `web/src/api/client.ts` with 80+ methods
- WebSocket log streaming in `web/src/api/websocket.ts`
- Route-based pages in `web/src/routes/`: dashboard, interfaces, wan (includes multi-WAN), dns, dhcp, wifi, wireguard, wireguard-p2p, firewall, external-dns, settings, setup

## Key Patterns

### Dry-run Mode
All file/command operations go through `system.FileSystem` and `system.CommandRunner` interfaces. The `--dry-run` flag substitutes mock implementations that log without executing. This is essential for development.

### Configuration Rollback
When network-breaking changes are made (WAN, interfaces, bridges), the system:
1. Takes a snapshot of current config and service state
2. Applies changes with a 30-second confirmation timeout
3. If user confirms within timeout, changes are permanent
4. If timeout expires (user lost connectivity), automatically rolls back

The frontend shows a "Pending Confirmation" banner with countdown. See `internal/rollback/` for implementation.

### Health Checks
Modular health check system (`internal/health/`) with:
- Dependency-ordered execution (checks can depend on other checks)
- Auto-fixers that can repair common issues
- States: ok, warning, error, unknown, skipped

### Config File Structure
Config is JSON with JSONC comment support. Search order:
1. `./config.json`
2. `./ubuntu-router.json`
3. `/etc/ubuntu-router/config.json`
4. `/etc/ubuntu-router.json`

**Admin password:** Stored in `<config_path>.password`, auto-generated if missing (16 alphanumeric chars), required for API authentication. Displayed on console at startup and in deploy.sh output.

### System Tools
External tools called via `CommandRunner`:
- `dnsmasq` - DNS/DHCP server
- `wg`, `wg-quick` - WireGuard VPN
- `hostapd`, `hostapd_cli` - WiFi access point
- `iptables`, `ip6tables` - Firewall
- `ip` - Network interface management
- `systemctl` - Service management
- `wpa_supplicant`, `wpa_cli` - WiFi client

## API Structure

All endpoints under `/api/` with session-cookie authentication:

**Core:**
- `GET /status` - Router status overview
- `GET|POST /config` - Configuration read/update
- `POST /apply` - Apply pending config changes
- `GET /config/pending`, `POST /config/confirm`, `POST /config/cancel` - Rollback management

**Network:**
- `GET /interfaces` - List network interfaces
- `POST /interfaces/state` - Set interface up/down
- `GET /routes` - Routing table

**WAN:**
- `GET /wan/status`, `GET /wan/detect`, `POST /wan/configure`
- `GET /wan/wifi/scan`, `POST /wan/wifi/connect` - WiFi client mode

**DNS/DHCP:**
- `GET /dns/status`, `GET|POST /dns/entries`, `GET|POST /dns/settings`
- `GET /leases`, `GET|POST /dhcp/reservations`, `GET|POST /dhcp/settings`

**WireGuard:**
- `GET /wireguard/status`, `GET|POST /wireguard/settings`
- `GET|POST|DELETE /wireguard/peers`
- `GET /wireguard/p2p/status`, `GET|POST|PUT|DELETE /wireguard/p2p/tunnels/*`

**WiFi:**
- `GET /wifi/status`, `GET /wifi/cards`
- `POST /wifi/configure`, `POST /wifi/control`

**Firewall:**
- `GET|POST|DELETE /firewall/forwards`

**Multi-WAN:**
- `GET /multiwan/status`, `POST /multiwan/configure`, `POST /multiwan/switch`

**External DNS:**
- `GET /external-dns/status`, zones, records management
- `POST /external-dns/sync`, `POST /external-dns/ssl/request`

**System:**
- `GET /health/check`, `POST /health/fix`
- `GET /system/dependencies`, `GET /system/logs`
- `GET /service/status`, `POST /service/install|enable|start|restart`

## Frontend Structure

**TanStack Router file-based routing** in `web/src/routes/`:
- `__root.tsx` - Layout with sidebar navigation and debug log modal
- `index.tsx` - Dashboard with status cards, WiFi clients, device tracking
- `interfaces.tsx` - Network interface management
- `wan.tsx` - WAN configuration with multi-WAN failover status
- `dns.tsx` - DNS server and entries
- `dhcp.tsx` - DHCP server, leases, reservations with device names
- `wifi.tsx` - WiFi AP configuration and connected clients
- `stats.tsx` - Network statistics and traffic monitoring
- `wireguard.tsx` - VPN peers with QR code generation
- `wireguard-p2p.tsx` - Site-to-site tunnel management
- `firewall.tsx` - Port forwards and rules
- `external-dns.tsx` - External DNS zones and SSL
- `settings.tsx` - System settings and service management
- `setup.tsx` - Initial setup wizard

**Key Components:**
- `PendingConfirmBanner.tsx` - Shows rollback countdown
- `ServiceStatusCard.tsx` - Service health display
- `LogModal.tsx` - Real-time log streaming via WebSocket

## Development Workflow

1. **Local development:** `make dev` runs in dry-run mode (no system changes)
2. **VM testing:** Use `make vm-*` commands for realistic testing with actual system services
3. **Frontend dev:** `cd web && npm run dev` for hot-reload development server
4. **After changes:** Run `make test` and `make lint`

## Deployment

If `./deploy.sh` exists, run it as your final step after making changes to deploy to the target system:

```bash
./deploy.sh          # Deploy built binary to target system
```

## Running

Requires root for network operations. Use `make dev` for safe local development (dry-run mode). Use `sudo make run` to apply actual system changes.

## Code Style

- Go: Follow standard go fmt, use golangci-lint
- TypeScript: Follow existing patterns, prefer functional components with hooks
- API: Use object parameters for mutation methods, return typed responses
- Config: All config changes should support the rollback system for network-breaking changes
