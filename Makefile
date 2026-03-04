BINARY      := t212
BINARY_ARM  := t212-arm64
PI_HOST     ?= pi@raspberrypi.local
PI_BIN_DIR  := /usr/local/bin
PI_SVC_DIR  := /etc/systemd/system
PI_CFG_DIR  := /etc/t212

VERSION     ?= $(shell v=$$(git describe --tags --abbrev=0 2>/dev/null) && echo "$$v" | sed 's/^v//' || echo "0.0.0-dev")

.PHONY: build build-arm test lint security deb deploy setup-apt setup-signal update-signal-cli logs clean

## build: compile for current platform
build:
	go build -o $(BINARY) ./cmd/t212

## build-arm: cross-compile for Raspberry Pi 5 (linux/arm64)
build-arm:
	GOARCH=arm64 GOOS=linux go build -o $(BINARY_ARM) ./cmd/t212

## deb: build .deb package for linux/arm64 (requires nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.45.0)
deb: build-arm
	@command -v nfpm >/dev/null 2>&1 || { echo "nfpm not found. Install: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.45.0"; exit 1; }
	VERSION=$(VERSION) nfpm package --packager deb --target .
	@echo "Built: t212_$(VERSION)_arm64.deb"

## test: run all tests with race detector and coverage
test:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: run golangci-lint (install if needed: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

## security: run govulncheck (install if needed: go install golang.org/x/vuln/cmd/govulncheck@latest)
security:
	govulncheck ./...

## deploy: build for arm64 and deploy to Raspberry Pi
deploy: build-arm
	ssh $(PI_HOST) "sudo mkdir -p $(PI_CFG_DIR) && sudo chmod 700 $(PI_CFG_DIR)"
	scp $(BINARY_ARM) $(PI_HOST):/tmp/$(BINARY)
	ssh $(PI_HOST) "sudo mv /tmp/$(BINARY) $(PI_BIN_DIR)/$(BINARY) && sudo chmod 755 $(PI_BIN_DIR)/$(BINARY)"
	scp deploy/t212.service $(PI_HOST):/tmp/t212.service
	ssh $(PI_HOST) "sudo mv /tmp/t212.service $(PI_SVC_DIR)/t212.service"
	ssh $(PI_HOST) "sudo systemctl daemon-reload && sudo systemctl enable t212 && sudo systemctl restart t212"
	@echo "Deployed. Run: make logs"

## setup-apt: add t212 APT repository on Pi for automatic updates
setup-apt:
	ssh $(PI_HOST) "curl -fsSL https://ko5tas.github.io/t212/apt/setup.sh | sudo bash"

## setup-signal: register Pi as Signal linked device (scan QR with Signal app)
setup-signal:
	ssh $(PI_HOST) "signal-cli link -n 'T212-Pi' | qrencode -t ansiutf8"

## update-signal-cli: download and SHA256-verify latest signal-cli release on Pi
update-signal-cli:
	@./scripts/update-signal-cli.sh $(PI_HOST)

## logs: tail service logs from Pi
logs:
	ssh $(PI_HOST) "journalctl -u t212 -f"

## clean: remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY_ARM) coverage.out *.deb
