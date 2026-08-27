#!/usr/bin/env bash
# Antigravity Runner Script for Ubuntu Router (Linux / macOS)

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error(){ echo -e "${RED}[X]${NC} $1"; exit 1; }

# Parse arguments
BUILD_WEB=false
LIVE_MODE=false

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --build-web) BUILD_WEB=true ;;
        --live) LIVE_MODE=true ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
    shift
done

# Ensure Go is present
if ! command -v go &> /dev/null; then
    if [ -f "/usr/local/go/bin/go" ]; then
        export PATH=$PATH:/usr/local/go/bin
    else
        error "Go is not installed or not in PATH."
    fi
fi
log "Found Go: $(go version)"

# Ensure Node/npm is present
if ! command -v npm &> /dev/null; then
    error "npm is not installed."
fi
log "Found npm: $(npm --version)"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Build Web Frontend if missing or requested
if [ ! -d "web/dist" ] || [ "$BUILD_WEB" = true ]; then
    log "Building Web Frontend..."
    (cd web && npm install && npm run build)
else
    log "Web Frontend dist folder found."
fi

# Build Go Backend
log "Building Go Backend binary..."
CGO_ENABLED=0 go build -o ubuntu-router ./cmd/ubuntu-router
log "Backend binary built successfully!"

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Ubuntu Router (Antigravity Runner)    ${NC}"
echo -e "${CYAN}  URL: http://localhost:8080           ${NC}"
echo -e "${CYAN}  Mode: $(if [ "$LIVE_MODE" = true ]; then echo 'LIVE'; else echo 'DRY-RUN'; fi)${NC}"
echo -e "${CYAN}========================================${NC}"

if [ "$LIVE_MODE" = true ]; then
    sudo ./ubuntu-router
else
    ./ubuntu-router --dry-run
fi
