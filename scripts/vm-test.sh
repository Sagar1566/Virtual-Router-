#!/bin/bash
set -e

VM_NAME="ubuntu-router-test"
UBUNTU_VERSION="24.04"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }

# Check if multipass is installed
if ! command -v multipass &> /dev/null; then
    error "Multipass is not installed. Install with: sudo snap install multipass"
fi

# Parse arguments
ACTION="${1:-help}"

case "$ACTION" in
    create)
        log "Creating VM: $VM_NAME with Ubuntu $UBUNTU_VERSION"

        # Check if VM already exists
        if multipass list | grep -q "$VM_NAME"; then
            warn "VM $VM_NAME already exists. Use 'destroy' first or 'shell' to connect."
            exit 1
        fi

        # Create VM with 2 CPUs, 2GB RAM, 10GB disk
        multipass launch "$UBUNTU_VERSION" --name "$VM_NAME" --cpus 2 --memory 2G --disk 10G

        log "Installing dependencies..."
        multipass exec "$VM_NAME" -- sudo apt-get update
        multipass exec "$VM_NAME" -- sudo apt-get install -y hostapd dnsmasq wireguard iptables iproute2 iw linux-modules-extra-\$(uname -r)

        # Stop default services that might conflict
        multipass exec "$VM_NAME" -- sudo systemctl stop hostapd || true
        multipass exec "$VM_NAME" -- sudo systemctl stop dnsmasq || true
        multipass exec "$VM_NAME" -- sudo systemctl disable hostapd || true
        multipass exec "$VM_NAME" -- sudo systemctl disable dnsmasq || true

        log "VM created and dependencies installed!"
        log "Use './scripts/vm-test.sh deploy' to deploy ubuntu-router"
        log "Use './scripts/vm-test.sh wifi-sim' to create simulated WiFi interfaces for testing"
        ;;

    wifi-sim)
        log "Creating simulated WiFi interfaces using mac80211_hwsim..."

        # Load the kernel module with 2 radios
        multipass exec "$VM_NAME" -- sudo modprobe mac80211_hwsim radios=2 || {
            error "Failed to load mac80211_hwsim. The kernel module may not be available."
        }

        # Wait for interfaces to appear
        sleep 2

        # Show the created interfaces
        log "Created simulated WiFi interfaces:"
        multipass exec "$VM_NAME" -- iw dev

        log "You can now use wlan0 and wlan1 for WiFi testing"
        log "Note: wlan0 can be an AP, wlan1 can be a client"
        ;;

    wifi-sim-remove)
        log "Removing simulated WiFi interfaces..."
        multipass exec "$VM_NAME" -- sudo modprobe -r mac80211_hwsim || true
        log "Simulated WiFi interfaces removed"
        ;;

    deploy)
        log "Building ubuntu-router..."
        cd "$(dirname "$0")/.."
        make build

        # Build web if it exists
        if [ -d "web" ] && [ -f "web/package.json" ]; then
            log "Building web frontend..."
            (cd web && npm install && npm run build) || warn "Web build failed, continuing without frontend"
        fi

        log "Deploying to VM..."
        multipass transfer ubuntu-router "$VM_NAME":/tmp/ubuntu-router
        multipass exec "$VM_NAME" -- sudo mv /tmp/ubuntu-router /usr/local/bin/ubuntu-router
        multipass exec "$VM_NAME" -- sudo chmod +x /usr/local/bin/ubuntu-router

        # Transfer web dist if it exists
        if [ -d "web/dist" ]; then
            log "Deploying web frontend..."
            multipass transfer -r web/dist "$VM_NAME":/tmp/web-dist
            multipass exec "$VM_NAME" -- sudo mkdir -p /usr/local/share/ubuntu-router
            multipass exec "$VM_NAME" -- sudo rm -rf /usr/local/share/ubuntu-router/dist
            multipass exec "$VM_NAME" -- sudo mv /tmp/web-dist /usr/local/share/ubuntu-router/dist
        fi

        log "Deployed! Use './scripts/vm-test.sh run' to start ubuntu-router"
        ;;

    run)
        log "Running ubuntu-router in VM (dry-run mode)..."
        log "Web UI will be available at: http://$(multipass info "$VM_NAME" | grep IPv4 | awk '{print $2}'):8080"
        multipass exec "$VM_NAME" -- sudo /usr/local/bin/ubuntu-router --dry-run
        ;;

    run-live)
        log "Running ubuntu-router in VM (LIVE mode - will modify system)..."
        log "Web UI will be available at: http://$(multipass info "$VM_NAME" | grep IPv4 | awk '{print $2}'):8080"
        multipass exec "$VM_NAME" -- sudo /usr/local/bin/ubuntu-router
        ;;

    shell)
        log "Opening shell in VM..."
        multipass shell "$VM_NAME"
        ;;

    ip)
        IP=$(multipass info "$VM_NAME" | grep IPv4 | awk '{print $2}')
        echo "$IP"
        ;;

    logs)
        log "Showing logs from VM..."
        multipass exec "$VM_NAME" -- sudo cat /var/log/ubuntu-router.log 2>/dev/null || \
        multipass exec "$VM_NAME" -- cat ./ubuntu-router.log 2>/dev/null || \
        warn "No logs found"
        ;;

    stop)
        log "Stopping ubuntu-router in VM..."
        multipass exec "$VM_NAME" -- sudo pkill ubuntu-router || true
        log "Stopped"
        ;;

    status)
        log "VM Status:"
        multipass info "$VM_NAME"
        echo ""
        log "ubuntu-router process:"
        multipass exec "$VM_NAME" -- pgrep -a ubuntu-router || echo "Not running"
        ;;

    destroy)
        warn "This will delete the VM and all its data!"
        read -p "Are you sure? (y/N) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            log "Destroying VM..."
            multipass delete "$VM_NAME"
            multipass purge
            log "VM destroyed"
        fi
        ;;

    check)
        log "Checking system requirements in VM..."
        multipass exec "$VM_NAME" -- /usr/local/bin/ubuntu-router --check
        ;;

    *)
        echo "Ubuntu Router VM Testing Script"
        echo ""
        echo "Usage: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  create          - Create a new Ubuntu $UBUNTU_VERSION VM with dependencies"
        echo "  deploy          - Build and deploy ubuntu-router to the VM"
        echo "  run             - Run ubuntu-router in dry-run mode"
        echo "  run-live        - Run ubuntu-router in live mode (modifies system)"
        echo "  shell           - Open a shell in the VM"
        echo "  ip              - Print the VM's IP address"
        echo "  logs            - Show ubuntu-router logs from VM"
        echo "  stop            - Stop ubuntu-router in the VM"
        echo "  status          - Show VM and process status"
        echo "  check           - Run system requirements check"
        echo "  destroy         - Delete the VM"
        echo ""
        echo "WiFi Testing:"
        echo "  wifi-sim        - Create simulated WiFi interfaces (wlan0, wlan1)"
        echo "  wifi-sim-remove - Remove simulated WiFi interfaces"
        echo ""
        echo "Quick start:"
        echo "  $0 create       # Create VM and install deps"
        echo "  $0 deploy       # Build and deploy"
        echo "  $0 wifi-sim     # Create simulated WiFi for testing"
        echo "  $0 run-live     # Start in live mode"
        ;;
esac
