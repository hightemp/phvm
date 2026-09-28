# phvm - PHP Version Manager

[![GitHub release](https://img.shields.io/github/v/release/hightemp/phvm)](https://github.com/hightemp/phvm/releases/latest)
[![GitHub downloads](https://img.shields.io/github/downloads/hightemp/phvm/total)](https://github.com/hightemp/phvm/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/hightemp/phvm)](https://goreportcard.com/report/github.com/hightemp/phvm)
![](https://asdertasd.site/counter/phvm)

A fast, cross-platform PHP version manager inspired by [nvm](https://github.com/nvm-sh/nvm). Install PHP from source, manage multiple versions, and switch between them seamlessly.

## Features

- **Install PHP from source** with configurable build profiles (minimal, common, full)
- **Switch PHP versions** instantly via symlinks
- **Manage php.ini** settings with profiles (development, production)
- **Install PECL extensions** with automatic phpize/configure/make
- **Shell integration** for bash, zsh, fish, and PowerShell
- **Cross-platform** support for Linux, macOS, and Windows

## Quick Start

### Installation

**Linux/macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.sh | bash
```

**Windows (PowerShell):**

```powershell
iwr -useb https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.ps1 | iex
```

### Shell Setup

Add to your shell profile:

**Bash (~/.bashrc):**
```bash
eval "$(~/.phvm/bin/phvm init bash)"
```

**Zsh (~/.zshrc):**
```zsh
eval "$(~/.phvm/bin/phvm init zsh)"
```

**Fish (~/.config/fish/config.fish):**
```fish
~/.phvm/bin/phvm init fish | source
```

**PowerShell ($PROFILE):**
```powershell
Invoke-Expression (& ~/.phvm/bin/phvm init powershell)
```

### Basic Usage

```bash
# Check system dependencies
phvm doctor

# List available PHP versions
phvm ls-remote

# Install a PHP version
phvm install 8.3

# Install specific version with build profile
phvm install 8.3.15 --profile full

# List installed versions
phvm ls

# Switch to a version
phvm use 8.3.15

# Set default version
phvm alias default 8.3.15

# Show current version
phvm current
```

## Commands

### Version Management

| Command | Description |
|---------|-------------|
| `phvm install <version>` | Install a PHP version |
| `phvm uninstall <version>` | Remove an installed version |
| `phvm use <version>` | Switch to a version |
| `phvm current` | Show active version |
| `phvm ls` | List installed versions |
| `phvm ls-remote` | List available versions |

### Aliases

`phvm use` resolves only installed PHP releases. A full version selects that
exact release; `8.3` selects the highest installed `8.3.x`, and `8` selects the
highest installed `8.x.x`. The `v` and `php-` prefixes are accepted for full and
partial versions. Missing versions and corrupt installations are rejected.

Local aliases take precedence and may point to another alias or a partial
version. Cycles, corrupt alias files and chains longer than 10 links are errors.
Without a local override, `latest` and `stable` select the newest installed
release without contacting php.net. `lts` must be defined as a local alias.

```bash
phvm alias prod 8.3
phvm use prod
phvm use latest
phvm composer disable --php 8.3
phvm ext disable redis --php prod
```

The `--php` flags for Composer and extensions use the same installed-version
resolver. They select the target for that operation and do not switch `current`.
When `--php` is omitted, the current installed version is used. A failed `use`
leaves the previous current version intact.

| Command | Description |
|---------|-------------|
| `phvm alias <name> <version>` | Create an alias |
| `phvm alias rm <name>` | Remove an alias |
| `phvm alias ls` | List all aliases |

### Extensions

| Command | Description |
|---------|-------------|
| `phvm ext install <name>` | Install PECL extension |
| `phvm ext remove <name>` | Remove extension |
| `phvm ext enable <name>` | Enable extension |
| `phvm ext disable <name>` | Disable extension |
| `phvm ext ls` | List installed extensions |

### Configuration

| Command | Description |
|---------|-------------|
| `phvm ini get <key>` | Get php.ini value |
| `phvm ini set <key> <value>` | Set php.ini value |
| `phvm ini profile <name>` | Apply ini profile (development/production) |
| `phvm ini edit` | Open php.ini in editor |

### Other Commands

| Command | Description |
|---------|-------------|
| `phvm doctor` | Check build dependencies |
| `phvm composer install` | Install Composer |
| `phvm cache clean` | Clear download cache |
| `phvm init <shell>` | Print shell init script |

## Build Profiles

When installing PHP, you can choose a build profile:

- **minimal** - Core PHP only, smallest footprint
- **common** - Common extensions (curl, json, mbstring, openssl, etc.)
- **full** - All available extensions

```bash
# Install with minimal profile
phvm install 8.3 --profile minimal

# Install with full profile
phvm install 8.3 --profile full
```

## php.ini Profiles

Switch between development and production configurations:

```bash
# Apply development settings (display_errors=On, etc.)
phvm ini profile development

# Apply production settings (display_errors=Off, etc.)
phvm ini profile production
```

## Directory Structure

```
~/.phvm/
├── bin/                    # phvm binary
├── versions/
│   └── php/
│       ├── 8.3.15/        # Installed PHP version
│       └── 8.2.27/
├── current -> versions/php/8.3.15  # Active version symlink
├── alias/
│   └── default            # Alias files
├── cache/
│   └── downloads/         # Downloaded tarballs
├── config/
│   └── config.toml        # phvm configuration
└── logs/                   # Build logs
```

## Configuration

Create `~/.phvm/config/config.toml`:

```toml
# Default build profile
default_profile = "common"

# Parallel make jobs
jobs = 4

# Verify GPG signatures
verify_gpg = true

# Custom configure flags
[configure]
flags = ["--with-pear"]
```

## Building from Source

### Prerequisites

- Go 1.27.1 or later (the required version is declared in `go.mod`)
- Git

### Build

```bash
git clone https://github.com/hightemp/phvm.git
cd phvm
make deps
make build
```

### Install

```bash
# Install to GOBIN
make install

# Or install to ~/.phvm/bin
make install-local
```

### Development

```bash
# Run tests
make test

# Run linter
make lint

# Format code
make fmt
```

### Security checks

```bash
# Known vulnerabilities in dependencies and the Go standard library
make govulncheck

# Potential security issues in application source code
make gosec

# Run both checks
make security
```

The tools run through `go run` with versions pinned in the Makefile:
govulncheck `v1.8.0`, gosec `v2.29.0`, and golangci-lint `v2.14.0`.
No separate global installation is required. The first run downloads the
tools and their dependencies to the Go module cache.

`govulncheck` and `gosec` run as separate CI jobs and return a nonzero exit
status when findings remain. Gosec findings require review and can include
false positives. Existing findings are not suppressed by this configuration.
Release packaging checks known vulnerabilities before building artifacts.
Go versions in CI and release builds are read from `go.mod`.

Integrity checks stop installation on a SHA256 mismatch, malformed Composer
checksum, or invalid/revoked GPG signature. Composer is downloaded to a unique
staging file and published only after verification; failures preserve the
previous PHAR. Concurrent failed downloads cannot change a verified PHAR.

PHP signatures use a fresh keyring from
`https://www.php.net/distributions/php-keyring.gpg`, independently of the source
mirror, in an isolated GPG home. Personal keys and configuration are not used.
With `verify.gpg_fallback_sha256 = true`, an unavailable GPG tool, missing
signature, or unavailable keyring can fall back to successful SHA256 verification.
An invalid signature always stops installation. Set the fallback option to
`false` to require GPG; setting `verify.gpg = false` explicitly disables it.
Installation metadata records the actual verification result, skip reason and
signing fingerprint.

Managed version paths require a complete `X.Y.Z` version (optional `v` or `php-`
prefixes are normalized). Alias/profile names use portable ASCII letters,
digits, dots, underscores and hyphens; traversal, reserved device names and
leading/trailing dots are rejected. Managed directories and installation
configuration paths cannot be redirected through symlinks. The `current` link
must point to a registered installation. HTTP diagnostics remove URL userinfo
and redact token/password/key/signature parameters throughout error chains and
the application logger.

### Release

`VERSION` contains the next release version in `X.Y.Z` format, without a `v`
prefix. Local builds also use this version for `phvm version`.

```bash
# Set the version, then publish it
printf '1.0.7\n' > VERSION
make release
```

`make release` stages all non-ignored changes, creates a `chore: release vX.Y.Z`
commit and an annotated `vX.Y.Z` tag, then pushes the current branch and tag to
`origin` atomically. The tag push triggers `.github/workflows/release.yml`.
Existing local or remote tags are rejected before committing. If the push
fails, the local commit and tag remain, and the command prints how to retry.
Update `VERSION` before each new release.

Test the automation using temporary local Git repositories:

```bash
make test-release
```

## PHP Build Requirements

Before installing PHP, ensure you have the required build dependencies:

```bash
# Check what's needed
phvm doctor
```

For libraries discovered through `pkg-config`, `doctor` compiles and links a
small C program using `CC`, `CPPFLAGS`, `CFLAGS`, and `LDFLAGS`. It does not run
the program. A `.pc` file alone is not enough: missing headers, libraries, or
linker dependencies are reported as `UNUSABLE`, with the compiler error and
the selected `.pc` file path.

### Ubuntu/Debian

```bash
sudo apt-get install -y \
    build-essential \
    autoconf \
    bison \
    re2c \
    libxml2-dev \
    libsqlite3-dev \
    libcurl4-openssl-dev \
    libonig-dev \
    libpng-dev \
    libjpeg-dev \
    libfreetype6-dev \
    libzip-dev \
    libssl-dev
```

### macOS

```bash
brew install \
    autoconf \
    bison \
    re2c \
    libxml2 \
    sqlite \
    curl \
    oniguruma \
    libpng \
    libjpeg \
    freetype \
    libzip \
    openssl@3
```

### Fedora/RHEL

```bash
sudo dnf install -y \
    gcc \
    gcc-c++ \
    make \
    autoconf \
    bison \
    re2c \
    libxml2-devel \
    sqlite-devel \
    libcurl-devel \
    oniguruma-devel \
    libpng-devel \
    libjpeg-devel \
    freetype-devel \
    libzip-devel \
    openssl-devel
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PHVM_DIR` | phvm installation directory | `~/.phvm` |
| `PHVM_VERSION` | Version to install (installer) | `latest` |

## Troubleshooting

### Build fails with missing dependencies

Run `phvm doctor` to identify missing dependencies, then install them using your package manager.

When `configure` fails, phvm prints the last output lines and the path to
`config.log`, which contains the compiler and linker errors. The full output
is also saved in `~/.phvm/logs/install-<version>.log` (or `$PHVM_DIR/logs`).

### libcurl is reported by pkg-config but PHP cannot link it

`configure: error: The libcurl check failed` can occur when an old
`/usr/local/lib/pkgconfig/libcurl.pc` remains after its library was removed.
Having the curl command or a runtime `libcurl.so.4` does not supply the headers
and `libcurl.so` linker file required for a build.

On Ubuntu/Debian, install the development package:

```bash
sudo apt-get install libcurl4-openssl-dev
```

If Homebrew tools or stale `/usr/local` metadata interfere with system
dependencies, select the system tools and pkg-config directories for this
command. For Ubuntu/Debian on amd64:

```bash
PATH="/usr/bin:/bin:$PATH" \
PKG_CONFIG_PATH= \
PKG_CONFIG_LIBDIR="/usr/lib/x86_64-linux-gnu/pkgconfig:/usr/lib/pkgconfig:/usr/share/pkgconfig" \
phvm install 8.5.11 --jobs 2
```

Use the same environment with `phvm doctor` to check the dependencies before
building. On other architectures, replace `x86_64-linux-gnu` with the system's
multiarch directory name (`cc -print-multiarch`).

### Permission denied errors

Ensure `~/.phvm` is owned by your user:

```bash
sudo chown -R $USER:$USER ~/.phvm
```

### PHP not found after installation

Make sure you've added the shell init script to your profile and restarted your shell.

### Extension installation fails

Check that phpize is available and development headers are installed:

```bash
phvm doctor
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

- Inspired by [nvm](https://github.com/nvm-sh/nvm) for Node.js
- PHP source from [php.net](https://www.php.net/)
- Extensions from [PECL](https://pecl.php.net/)
