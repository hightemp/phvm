# phvm Makefile

BINARY_NAME := phvm
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-s -w -X github.com/hightemp/phvm/internal/cli.Version=$(VERSION) -X github.com/hightemp/phvm/internal/cli.Commit=$(COMMIT)"

GO := go
GOFLAGS := -trimpath
GOLANGCI_LINT_VERSION := $(shell python3 scripts/tool_version.py golangci-lint)
GOVULNCHECK_VERSION := $(shell python3 scripts/tool_version.py govulncheck)
GOSEC_VERSION := $(shell python3 scripts/tool_version.py gosec)
GORELEASER_VERSION := $(shell python3 scripts/tool_version.py goreleaser)
ACTIONLINT_VERSION := $(shell python3 scripts/tool_version.py actionlint)
GORELEASER := $(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

.PHONY: all build build-all clean test test-release test-installers test-checks test-scenarios test-runtime-scenarios test-native-scenarios test-native-build release release-check release-package-check release-config-check lint fmt vet verify security security-baseline govulncheck gosec install uninstall help

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
	GOOS=windows GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY_NAME)_windows_arm64.exe ./cmd/phvm

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -rf dist/

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -cover ./...

# Test release automation against temporary local Git repositories
test-release:
	python3 -B -m unittest discover -s scripts/tests -p 'test_release.py' -v

# Test installer verification/publication with isolated mocked downloads
test-installers:
	python3 -B -m unittest discover -s scripts/tests -p 'test_installers.py' -v

test-checks:
	python3 -B -m unittest discover -s scripts/tests -p 'test_security_baseline.py' -v
	python3 -B -m unittest discover -s scripts/tests -p 'test_artifacts.py' -v
	python3 -B -m unittest discover -s scripts/tests -p 'test_scenario_report.py' -v

test-scenarios:
	python3 scripts/scenario_report.py --profile fixture

test-runtime-scenarios:
	python3 scripts/scenario_report.py --profile runtime-fixture

# Explicit upstream smoke; network and an existing real PHP SDK are required.
test-native-scenarios:
	python3 scripts/scenario_report.py --profile native

test-native-build:
	python3 scripts/scenario_report.py --profile native-build

# Read-only release preflight; snapshot artifacts are kept in ignored dist/.
release-check:
	git diff --check
	python3 scripts/check_release_version.py
	$(MAKE) release-config-check
	$(MAKE) verify vet lint test govulncheck security-baseline test-release test-installers test-checks
	$(MAKE) test-scenarios
	$(MAKE) release-package-check

release-config-check:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -shellcheck= -pyflakes=
	$(GORELEASER) check --config .goreleaser.yml

release-package-check:
	python3 scripts/check_release_version.py
	PHVM_RELEASE_VERSION=$(VERSION) $(GORELEASER) release --snapshot --clean --config .goreleaser.yml
	python3 scripts/check_release_artifacts.py --dist dist --version $(VERSION)

# Commit and publish the version from VERSION, triggering the release workflow
release:
	@sh scripts/release.sh

# Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run the pinned linter without requiring a globally installed binary
lint:
	@echo "Running linter..."
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) config verify
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) fmt --diff
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

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

# Check known vulnerabilities and source security issues with pinned tools
security: govulncheck gosec

govulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

gosec:
	$(GO) run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) ./...

# Keep the full scanner report and fail only on new reviewed identities.
security-baseline:
	@mkdir -p reports
	@rm -f reports/gosec.json
	@status=0; $(GO) run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -fmt=json -out=reports/gosec.json ./... || status=$$?; \
	if [ $$status -gt 1 ]; then exit $$status; fi
	python3 scripts/check_security_baseline.py --report reports/gosec.json --baseline .gosec-baseline.json

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
	@echo "  test-release   Test release automation with local Git repositories"
	@echo "  test-installers Test release archive verification and installer publication"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  release        Commit all changes and push the VERSION tag to origin"
	@echo "  release-check  Validate source, security, workflows and six release archives without publishing"
	@echo "  release-package-check Build and smoke-test GoReleaser snapshot artifacts"
	@echo "  test-checks    Test archive and source security gates"
	@echo "  test-scenarios Run required fixture lifecycle scenarios and write evidence JSON"
	@echo "  test-runtime-scenarios Run real PHP with synthetic PHAR/C module fixtures"
	@echo "  test-native-scenarios Run upstream Composer/PECL lifecycle with real PHP SDK"
	@echo "  test-native-build Run isolated PHP source build using supplied archive/SHA256"
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
	@echo "  govulncheck    Check known dependency and Go vulnerabilities"
	@echo "  gosec          Check source code for security issues"
	@echo "  security-baseline Keep full gosec findings and reject identities absent from the reviewed baseline"
	@echo "  update-deps    Update dependencies"
	@echo "  help           Show this help"
