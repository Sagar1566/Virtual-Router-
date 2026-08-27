VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -s -w"

.PHONY: all build clean install run dev check fmt lint test build-web deploy vm-create vm-deploy vm-run vm-shell vm-destroy

all: build

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o ubuntu-router ./cmd/ubuntu-router

build-web:
	cd web && npm install && npm run build

build-static:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ubuntu-router ./cmd/ubuntu-router

clean:
	rm -f ubuntu-router
	rm -rf dist/
	rm -rf web/dist/

install: build
	sudo cp ubuntu-router /usr/local/bin/
	sudo chmod +x /usr/local/bin/ubuntu-router
	@echo "Installed to /usr/local/bin/ubuntu-router"
	@echo "Run 'sudo ubuntu-router --install' to install systemd service"

run: build
	sudo ./ubuntu-router

dev: build
	./ubuntu-router --dry-run

check:
	sudo ./ubuntu-router --check

fmt:
	go fmt ./...

lint:
	golangci-lint run

test:
	go test -v ./...

# Generate config template
config-template:
	./ubuntu-router --config-template > config.example.json

# Cross-compile for different architectures
dist: clean
	mkdir -p dist
	# amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/ubuntu-router-linux-amd64 ./cmd/ubuntu-router
	# arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/ubuntu-router-linux-arm64 ./cmd/ubuntu-router
	# arm (Raspberry Pi)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o dist/ubuntu-router-linux-armv7 ./cmd/ubuntu-router

# Deploy to remote host
deploy:
	./deploy.sh

# VM Testing (Multipass)
vm-create:
	./scripts/vm-test.sh create

vm-deploy: build build-web
	./scripts/vm-test.sh deploy

vm-run:
	./scripts/vm-test.sh run

vm-run-live:
	./scripts/vm-test.sh run-live

vm-shell:
	./scripts/vm-test.sh shell

vm-logs:
	./scripts/vm-test.sh logs

vm-status:
	./scripts/vm-test.sh status

vm-destroy:
	./scripts/vm-test.sh destroy

help:
	@echo "Ubuntu Router Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build the binary"
	@echo "  build-web      Build web frontend"
	@echo "  build-static   Build static binary for Linux amd64"
	@echo "  clean          Remove build artifacts"
	@echo "  install        Build and install to /usr/local/bin"
	@echo "  run            Build and run (requires sudo)"
	@echo "  dev            Build and run in dry-run mode"
	@echo "  check          Run system requirements check"
	@echo "  fmt            Format Go source code"
	@echo "  lint           Run linter"
	@echo "  test           Run tests"
	@echo "  dist           Cross-compile for multiple architectures"
	@echo "  deploy         Deploy to remote host (192.168.1.170)"
	@echo ""
	@echo "VM Testing (requires Multipass):"
	@echo "  vm-create      Create Ubuntu 24.04 VM with dependencies"
	@echo "  vm-deploy      Build and deploy to VM"
	@echo "  vm-run         Run in VM (dry-run mode)"
	@echo "  vm-run-live    Run in VM (live mode)"
	@echo "  vm-shell       Open shell in VM"
	@echo "  vm-logs        Show logs from VM"
	@echo "  vm-status      Show VM status"
	@echo "  vm-destroy     Delete the VM"
