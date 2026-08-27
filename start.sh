#!/usr/bin/env bash
# ==============================================================================
# All-in-One Setup & Run Script for Ubuntu Router (Virtual Router)
# Automatically checks, downloads, and configures all missing dependencies 
# (Go, Node.js, npm, Web Frontend, and Backend binary) and launches the project.
# ==============================================================================

set -e

# Terminal Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

log()   { echo -e "${GREEN}[+]${NC} $1"; }
info()  { echo -e "${BLUE}[i]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[X]${NC} $1"; exit 1; }

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

TOOLS_DIR="$HOME/.tools"
mkdir -p "$TOOLS_DIR"

# Parse CLI options
LIVE_MODE=false
REBUILD=false
PORT="8080"

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --live) LIVE_MODE=true ;;
        --rebuild) REBUILD=true ;;
        --port) PORT="$2"; shift ;;
        -h|--help)
            echo "Usage: ./start.sh [options]"
            echo ""
            echo "Options:"
            echo "  --live       Run in LIVE mode (requires root / applies real system changes)"
            echo "  --rebuild    Force clean rebuild of frontend and backend"
            echo "  --port <p>   Specify HTTP port (default: 8080)"
            echo "  -h, --help   Show this help message"
            exit 0
            ;;
        *) warn "Unknown option: $1 (ignoring)" ;;
    esac
    shift
done

echo -e "${CYAN}${BOLD}"
echo "========================================================"
echo "    UBUNTU ROUTER - ALL-IN-ONE AUTOMATED RUNNER         "
echo "========================================================"
echo -e "${NC}"

# Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)
        GO_ARCH="amd64"
        NODE_ARCH="x64"
        ;;
    aarch64|arm64)
        GO_ARCH="arm64"
        NODE_ARCH="arm64"
        ;;
    armv7l|armhf)
        GO_ARCH="armv6l"
        NODE_ARCH="armv7l"
        ;;
    *)
        error "Unsupported system architecture: $ARCH"
        ;;
esac

# ------------------------------------------------------------------------------
# 1. Check / Install Go
# ------------------------------------------------------------------------------
export PATH="$TOOLS_DIR/go/bin:/usr/local/go/bin:$PATH"

if ! command -v go &> /dev/null; then
    warn "Go compiler not found. Automatically downloading portable Go for $GO_ARCH..."
    GO_VERSION="1.22.6"
    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TAR}"
    
    info "Downloading $GO_URL..."
    curl -fsSL "$GO_URL" -o "/tmp/$GO_TAR"
    
    info "Extracting Go to $TOOLS_DIR/go..."
    rm -rf "$TOOLS_DIR/go"
    tar -C "$TOOLS_DIR" -xzf "/tmp/$GO_TAR"
    rm -f "/tmp/$GO_TAR"
    
    export PATH="$TOOLS_DIR/go/bin:$PATH"
    log "Go installed successfully: $(go version)"
else
    log "Go is available: $(go version)"
fi

# ------------------------------------------------------------------------------
# 2. Check / Install Node.js & npm
# ------------------------------------------------------------------------------
export PATH="$TOOLS_DIR/node/bin:$PATH"

if ! command -v node &> /dev/null || ! command -v npm &> /dev/null; then
    warn "Node.js/npm not found. Automatically downloading portable Node.js LTS for $NODE_ARCH..."
    NODE_VERSION="20.18.0"
    NODE_DIR_NAME="node-v${NODE_VERSION}-linux-${NODE_ARCH}"
    NODE_TAR="${NODE_DIR_NAME}.tar.xz"
    NODE_URL="https://nodejs.org/dist/v${NODE_VERSION}/${NODE_TAR}"
    
    info "Downloading $NODE_URL..."
    curl -fsSL "$NODE_URL" -o "/tmp/$NODE_TAR"
    
    info "Extracting Node.js to $TOOLS_DIR/node..."
    tar -C "/tmp" -xf "/tmp/$NODE_TAR"
    rm -rf "$TOOLS_DIR/node"
    mv "/tmp/$NODE_DIR_NAME" "$TOOLS_DIR/node"
    rm -f "/tmp/$NODE_TAR"
    
    export PATH="$TOOLS_DIR/node/bin:$PATH"
    log "Node.js & npm installed successfully: Node $(node -v), npm $(npm -v)"
else
    log "Node.js & npm are available: Node $(node -v), npm $(npm -v)"
fi

# Ensure PATH persistence in user environment
if [ -f "$HOME/.bashrc" ] && ! grep -q "/.tools/node/bin" "$HOME/.bashrc"; then
    echo 'export PATH="$HOME/.tools/node/bin:$HOME/.tools/go/bin:$PATH"' >> "$HOME/.bashrc"
fi

# ------------------------------------------------------------------------------
# 3. Build Web Frontend (React + TypeScript SPA)
# ------------------------------------------------------------------------------
if [ ! -d "web/dist" ] || [ ! -d "web/node_modules" ] || [ "$REBUILD" = true ]; then
    info "Building React/TypeScript Web Dashboard frontend..."
    cd web
    npm install
    npm run build
    cd "$SCRIPT_DIR"
    log "Web frontend built cleanly into web/dist!"
else
    log "Web frontend build (web/dist) is up to date."
fi

# ------------------------------------------------------------------------------
# 4. Build Backend Binary (Go)
# ------------------------------------------------------------------------------
if [ ! -f "ubuntu-router" ] || [ "$REBUILD" = true ]; then
    info "Compiling Go backend executable (ubuntu-router)..."
    CGO_ENABLED=0 go build -o ubuntu-router ./cmd/ubuntu-router
    log "Backend binary compiled successfully!"
else
    log "Backend binary (ubuntu-router) is ready."
fi

# ------------------------------------------------------------------------------
# 5. Display Startup Banner & Launch
# ------------------------------------------------------------------------------
PASSWORD_FILE="config.json.password"
PASSWORD=""
if [ -f "$PASSWORD_FILE" ]; then
    PASSWORD="$(cat "$PASSWORD_FILE" | tr -d '\r\n')"
fi

echo ""
echo -e "${CYAN}${BOLD}========================================================${NC}"
echo -e "${CYAN}${BOLD}           UBUNTU ROUTER IS READY TO RUN!               ${NC}"
echo -e "${CYAN}${BOLD}========================================================${NC}"
echo -e " ${BOLD}Web Dashboard:${NC}    ${GREEN}http://localhost:${PORT}${NC}"
if [ -n "$PASSWORD" ]; then
echo -e " ${BOLD}Admin Password:${NC}   ${YELLOW}${PASSWORD}${NC}"
fi
echo -e " ${BOLD}Execution Mode:${NC}   $(if [ "$LIVE_MODE" = true ]; then echo -e "${RED}LIVE (Real system changes)${NC}"; else echo -e "${GREEN}DRY-RUN (Safe Development Mode)${NC}"; fi)"
echo -e " ${BOLD}Config File:${NC}      ${SCRIPT_DIR}/config.json"
echo -e "${CYAN}${BOLD}========================================================${NC}"
echo ""

if [ "$LIVE_MODE" = true ]; then
    info "Starting in LIVE mode with sudo..."
    sudo ./ubuntu-router --listen ":${PORT}"
else
    info "Starting server in DRY-RUN mode..."
    ./ubuntu-router --dry-run --listen ":${PORT}"
fi
