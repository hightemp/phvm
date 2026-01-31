# phvm Makefile

BINARY_NAME := phvm
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

GO := go
GOFLAGS := -trimpath

.PHONY: all build clean test lint fmt vet install uninstall help

# Default target
all: lint test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/phvm

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_linux_amd64 ./cmd/phvm
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_linux_arm64 ./cmd/phvm
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_darwin_amd64 ./cmd/phvm
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_darwin_arm64 ./cmd/phvm
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_windows_amd64.exe ./cmd/phvm

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -rf dist/

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -cover ./...

# Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	$(GO) vet ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GO) mod verify

# Install to GOBIN
install: build
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(GOFLAGS) $(LDFLAGS) ./cmd/phvm

# Install to ~/.phvm/bin
install-local: build
	@echo "Installing to ~/.phvm/bin..."
	mkdir -p $(HOME)/.phvm/bin
	cp $(BINARY_NAME) $(HOME)/.phvm/bin/

# Uninstall from GOBIN
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	rm -f $(shell go env GOPATH)/bin/$(BINARY_NAME)

# Run the application
run: build
	./$(BINARY_NAME)

# Development: build and run
dev:
	$(GO) run ./cmd/phvm $(ARGS)

# Generate mocks (if needed)
generate:
	$(GO) generate ./...

# Check for security issues
security:
	@which gosec > /dev/null || (echo "Installing gosec..." && go install github.com/securego/gosec/v2/cmd/gosec@latest)
	gosec ./...

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	$(GO) get -u ./...
	$(GO) mod tidy

# Show help
help:
	@echo "phvm Makefile targets:"
	@echo ""
	@echo "  build          Build the binary"
	@echo "  build-all      Build for all platforms"
	@echo "  clean          Remove build artifacts"
	@echo "  test           Run tests"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  lint           Run golangci-lint"
	@echo "  fmt            Format code"
	@echo "  vet            Vet code"
	@echo "  deps           Download and tidy dependencies"
	@echo "  verify         Verify dependencies"
	@echo "  install        Install to GOBIN"
	@echo "  install-local  Install to ~/.phvm/bin"
	@echo "  uninstall      Uninstall from GOBIN"
	@echo "  run            Build and run"
	@echo "  dev            Run with go run (use ARGS=... for arguments)"
	@echo "  security       Run security checks"
	@echo "  update-deps    Update dependencies"
	@echo "  help           Show this help"
