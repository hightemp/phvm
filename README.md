# phvm - PHP Version Manager

![Experimental](https://img.shields.io/badge/status-experimental-orange)
![Vibe Coded](https://img.shields.io/badge/vibe-coded-blueviolet)

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

- Go 1.21 or later
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

## PHP Build Requirements

Before installing PHP, ensure you have the required build dependencies:

```bash
# Check what's needed
phvm doctor
```

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

![](https://asdertasd.site/counter/phvm)