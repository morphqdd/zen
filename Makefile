.PHONY: build all client server zenctl clean install test

# Build all binaries
all: client server zenctl

build: all

client:
	go build -o zen-client ./cmd/client.go

server:
	go build -o zen-server ./cmd/server.go

zenctl:
	go build -o zenctl ./cmd/zenctl.go

# Install to system
install: all
	sudo install -m 755 zen-client /usr/local/bin/
	sudo install -m 755 zen-server /usr/local/bin/
	sudo install -m 755 zenctl /usr/local/bin/
	sudo mkdir -p /etc/zen/clients
	@echo "✓ Installed to /usr/local/bin/"
	@echo "Run: sudo zenctl init --domain vpn.example.com"

# Initialize server
init:
	sudo ./zenctl init --domain vpn.example.com

# Cleanup
clean:
	rm -f zen-client zen-server zenctl
	sudo ip link delete zen-tun 2>/dev/null || true
	sudo ip link delete zen-srv 2>/dev/null || true

# Development
dev-client:
	go run ./cmd/client.go --config /etc/zen/clients/test.json

dev-server:
	go run ./cmd/server.go --config /etc/zen/server.conf

# Tests
test:
	go test -v ./...

test-encoding:
	go test -v ./internal/encoding/...

# Dependencies
deps:
	go mod tidy
	go mod download

# Help
help:
	@echo "Zen VPN - Makefile commands:"
	@echo ""
	@echo "  make build       - Build all binaries"
	@echo "  make install     - Install to /usr/local/bin"
	@echo "  make clean       - Remove binaries and TUN interfaces"
	@echo "  make test        - Run tests"
	@echo ""
	@echo "Quick start:"
	@echo "  1. make build"
	@echo "  2. sudo ./zenctl init --domain vpn.example.com"
	@echo "  3. sudo ./zenctl add-client alice"
	@echo "  4. sudo ./zen-server --config /etc/zen/server.conf"
