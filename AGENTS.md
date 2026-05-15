# AGENTS.md - Guidelines for AI Coding Agents

This document provides guidelines for AI agents working on the phvm (PHP Version Manager) codebase.

## Project Overview

phvm is a cross-platform PHP version manager written in Go 1.22. It installs PHP from source, manages multiple versions via symlinks, and handles extensions and php.ini configuration. The CLI is built with [cobra](https://github.com/spf13/cobra).

## Build & Development Commands

```bash
# Build
make build                    # Build the binary
make build-all                # Build for all platforms (linux/darwin/windows, amd64/arm64)

# Run without building
make dev ARGS="install 8.3"   # Run with go run

# Install
make install                  # Install to GOBIN
make install-local            # Install to ~/.phvm/bin

# Clean
make clean                    # Remove build artifacts

# Dependencies
make deps                     # Download and tidy dependencies
make update-deps              # Update all dependencies
```

## Linting & Formatting

```bash
make lint                     # Run golangci-lint (installs if missing)
make fmt                      # Format code with go fmt
make vet                      # Run go vet

# Direct golangci-lint (if installed)
golangci-lint run ./...
```

Enabled linters: `errcheck`, `gosimple`, `govet`, `ineffassign`, `staticcheck`, `unused`, `gofmt`, `goimports`, `misspell`, `unconvert`, `unparam`, `revive`.

## Testing

```bash
# Run all tests
make test                     # Runs: go test -v -race -cover ./...

# Run tests with coverage report
make test-coverage            # Generates coverage.html

# Run a single test
go test -v -run TestFunctionName ./internal/package/

# Run specific test case in table-driven test
go test -v -run TestFunctionName/test_case_name ./internal/package/

# Examples
go test -v -run TestParseVersion ./internal/core/
go test -v -run TestAtomicWriteFile ./internal/fsutil/
go test -v -run TestConfDManager/Create ./internal/ini/
```

## Code Style Guidelines

### Imports

Order imports in three groups separated by blank lines:
1. Standard library
2. Third-party packages
3. Local packages (`github.com/hightemp/phvm/...`)

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/hightemp/phvm/internal/core"
    "github.com/hightemp/phvm/internal/log"
)
```

**Rules:**
- No dot imports (enforced by revive)
- Use `goimports` for automatic ordering

### Formatting

- Use `gofmt` with simplify enabled
- Tabs for indentation (Go standard)
- No trailing whitespace

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, short, descriptive | `cli`, `core`, `fsutil`, `remote` |
| Exported types | PascalCase | `Client`, `Paths`, `Version` |
| Unexported types | camelCase | `clientOptions` |
| Exported functions | PascalCase, verb prefix | `NewClient()`, `ParseVersion()` |
| Unexported functions | camelCase | `extractPriority()`, `normalizeIniName()` |
| Constants (exported) | PascalCase | `LevelDebug`, `SpecialVersionAliases` |
| Constants (unexported) | camelCase | `disabledSuffix` |
| Interfaces | PascalCase, `-er` suffix when applicable | `io.Reader`, `Locker` |
| Receivers | short, 1-2 letters | `(c *Client)`, `(p *Paths)`, `(v *Version)` |

### Error Handling

**Wrap errors with context:**
```go
if err != nil {
    return fmt.Errorf("create request: %w", err)
}
```

**Return early, avoid deep nesting:**
```go
func doSomething() error {
    if err := step1(); err != nil {
        return fmt.Errorf("step1: %w", err)
    }
    if err := step2(); err != nil {
        return fmt.Errorf("step2: %w", err)
    }
    return nil
}
```

**CLI error handling pattern:**
```go
func runCommand(cmd *cobra.Command, args []string) {
    if err := doWork(); err != nil {
        log.Error("Failed to do work: %v", err)
        os.Exit(1)
    }
    log.Success("Work completed")
}
```

### Comments & Documentation

**Package comments (required):**
```go
// Package core provides core functionality for phvm.
package core
```

**Exported function comments:**
```go
// ParseVersion parses a version string into a Version struct.
func ParseVersion(s string) (*Version, error) {
```

**Exported type comments:**
```go
// Client is an HTTP client with retry support.
type Client struct {
```

### Testing Patterns

**Table-driven tests with subtests:**
```go
func TestParseVersion(t *testing.T) {
    tests := []struct {
        input    string
        expected *Version
        wantErr  bool
    }{
        {"8.3.30", &Version{Major: 8, Minor: 3, Patch: 30}, false},
        {"", nil, true},
    }

    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            v, err := ParseVersion(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
            }
            // ... assertions
        })
    }
}
```

**Temp directory cleanup:**
```go
tmpDir, err := os.MkdirTemp("", "phvm-test-*")
if err != nil {
    t.Fatalf("Failed to create temp dir: %v", err)
}
defer os.RemoveAll(tmpDir)
```

## Project Structure

```
phvm/
├── cmd/phvm/main.go          # Entry point
├── internal/
│   ├── cli/                  # Cobra commands (root.go, install.go, etc.)
│   ├── core/                 # Core types: Paths, Version, Config, Alias
│   ├── build/                # PHP build logic and profiles
│   ├── remote/               # HTTP client, php.net API, PECL
│   ├── fsutil/               # Filesystem utilities (atomic writes, symlinks)
│   ├── ini/                  # php.ini management, conf.d
│   ├── ext/                  # Extension installation
│   ├── shell/                # Shell init scripts
│   ├── composer/             # Composer installation
│   ├── doctor/               # Dependency checking
│   └── log/                  # Structured logging
├── scripts/                  # Install scripts (bash, PowerShell)
├── Makefile                  # Build automation
├── .golangci.yml             # Linter configuration
└── .goreleaser.yml           # Release configuration
```

## Dependencies

Key dependencies (see `go.mod`):
- `github.com/spf13/cobra` - CLI framework
- `github.com/Masterminds/semver/v3` - Semantic versioning
- `github.com/fatih/color` - Terminal colors
- `github.com/hashicorp/go-retryablehttp` - HTTP client with retries
- `github.com/pelletier/go-toml/v2` - TOML config parsing
- `github.com/schollz/progressbar/v3` - Progress bars

## Common Patterns

**Manager pattern for domain logic:**
```go
type InstalledManager struct {
    paths *Paths
}

func NewInstalledManager(paths *Paths) *InstalledManager {
    return &InstalledManager{paths: paths}
}

func (m *InstalledManager) IsInstalled(version string) bool { ... }
```

**Options pattern for configuration:**
```go
type ClientOptions struct {
    UserAgent string
    Timeout   time.Duration
    Retries   int
}

func DefaultClientOptions() ClientOptions { ... }
func NewClient(opts ClientOptions) *Client { ... }
```

**Atomic file operations:** Use `fsutil.AtomicWriteFile()` for safe file writes.
