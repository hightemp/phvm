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
| `phvm config show` | Show defaults plus TOML settings |
| `phvm config show --effective` | Show settings after environment and explicit CLI overrides |
| `phvm config validate` | Validate TOML, environment and explicit CLI settings |

### Other Commands

| Command | Description |
|---------|-------------|
| `phvm doctor --php 8.5.11 --profile common` | Check tools and libraries for the selected PHP build |
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
│   └── phvm.toml          # phvm configuration
└── logs/                   # Build logs
```

## Configuration

Create `~/.phvm/config/phvm.toml` (or `$PHVM_DIR/config/phvm.toml`). Omitted
settings use the defaults shown below, except this example chooses four jobs
and adds `--with-pear`:

```toml
[general]
default_profile = "common"
parallel_jobs = 4
color = true

[remote]
mirror = "https://www.php.net"
user_agent = "phvm/1.0.0"
timeout = 60
retries = 3

[verify]
sha256 = true
gpg = true
gpg_fallback_sha256 = true

[build]
default_flags = ["--with-pear"]
```

The precedence is **explicit CLI flags → nonempty environment variables →
TOML → built-in defaults**. An omitted flag does not replace a configured value;
explicit `0` and `false` do. `--phvm-dir` takes precedence over `PHVM_DIR`.
All supplied TOML and environment values must be valid, even when a CLI flag
would override them. Unknown keys/tables, wrong types, invalid values and read
errors stop the command with a nonzero exit code. A missing file uses defaults.
`version` and `--help` remain available for diagnosis.

The old `config/config.toml` location is detected when `phvm.toml` is absent and
reported as an error. Move the file to `config/phvm.toml` and convert old
top-level `default_profile`, `jobs`, `verify_gpg` and `[configure].flags` to the
nested schema above. There is no silent fallback for a broken configuration.

| Setting | CLI override | Valid values / behavior |
|---------|--------------|-------------------------|
| `general.default_profile` | `install --profile`, `doctor --profile` | `minimal`, `common`, `full`; default PHP build profile |
| `general.parallel_jobs` | `install --jobs`, `ext install --jobs` | Nonnegative integer; `0` selects the builder default (PHP: half the CPUs, minimum 1; PECL: 2) |
| `general.color` | `--no-color[=false]` | Boolean; `--no-color` disables color, `--no-color=false` enables it |
| `remote.mirror` | `--mirror` | Absolute HTTP(S) base URL without query/fragment; PHP API and source downloads |
| `remote.user_agent` | `--user-agent` | Nonempty, single-line HTTP User-Agent |
| `remote.timeout` | `--timeout` | Integer seconds, 1–86400, for each HTTP attempt |
| `remote.retries` | `--retries` | Integer, 0–10; retries after the initial HTTP attempt |
| `verify.sha256` | None | Must be `true`; SHA256 is mandatory |
| `verify.gpg` | `--gpg`, `install --skip-gpg` | Boolean; PHP signature verification |
| `verify.gpg_fallback_sha256` | `--gpg-fallback-sha256` | Boolean; permit unavailable GPG verification only after successful SHA256 |
| `build.default_flags` | `install --configure`, `doctor --configure` | Array of nonempty, single-line PHP configure arguments |

Environment variables are listed below. `PHVM_CONFIGURE_FLAGS` replaces the
TOML flags array; explicit `--configure` arguments are merged over it, replacing
conflicting options. Profile flags are the base for the PHP build. These settings
do not supply configure arguments to PECL extensions.

```bash
phvm config validate
phvm config show
PHVM_JOBS=2 phvm config show --effective
phvm config show --effective --profile minimal --jobs 0 --gpg=false
PHVM_CONFIGURE_FLAGS='["--with-pdo-mysql"]' phvm install 8.3 --jobs 2
phvm install 8.3 --skip-gpg
```

`config show` prints defaults plus file settings without environment/CLI
overrides; `--effective` includes them. `config show --effective` and
`config validate` accept `--profile`, `--jobs` and `--configure` to inspect PHP
installation overrides. Displayed URL credentials are redacted.

`--skip-gpg` disables only GPG; SHA256 remains mandatory. `--skip-verify` is a
deprecated alias with the same behavior. Use one of `--gpg`, `--skip-gpg` or
`--skip-verify` per command; combining them is an error. Invalid or revoked
signatures always fail when GPG is enabled, including with fallback enabled.

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

Check the version and profile you intend to build:

```bash
phvm doctor
phvm doctor --php 8.5.11 --profile common
phvm doctor --php 8.5 --profile minimal
phvm doctor --php 8.5.11 --profile common --configure="--without-curl"
```

By default, `phvm doctor` checks the **current** PHP selected in the effective
`PHVM_DIR` (`--phvm-dir` takes precedence). `--php current` selects it explicitly.
A missing, broken or unsafe current produces exit code 1 with instructions to
use `phvm use <version>` or pass `--php X.Y[.Z]`; it never falls back to another
version or a default alias. Checking current does not switch it or run its PHP
binary.

`--php` also accepts an uninstalled `X.Y` branch or `X.Y.Z` release without
contacting php.net. An explicit version overrides current, including when no
current is set. A branch checks that branch's
requirements; use a full release to inspect its existing private dependency
directory. The profile and configure flags follow the configuration precedence
above. `minimal` disables default extensions; enabling an extension through
`--configure` adds its dependencies to the check. `common` requires its selected
curl/OpenSSL/zlib/XML/mbstring/bzip2/readline/SQLite/iconv libraries; `full` also
checks GD, ICU/C++, GMP, gettext, PostgreSQL, sodium, XSL and ZIP dependencies.

An unusable or missing required tool/library returns exit code **1**. Optional
tools produce warnings. `install` runs the same checks after version resolution
and before downloading PHP source or building dependencies; an already installed
version is left alone unless `--force` is requested.

Doctor prints `CC`, its executable/symlink target, the linker reported by the
compiler, `CXX`, `PKG_CONFIG`, selected `.pc` paths, `PATH`, compiler/linker flags
and pkg-config search variables. It uses the builder's merged configure flags
and environment, including existing private dependencies. Library checks compile
and link a small C program, including bzip2 (`BZ2_bzlibVersion`); they never run
the resulting program. A `.pc` file alone is not enough. Missing headers,
symbols, libraries and linker dependencies are reported as `UNUSABLE` with the
compiler error. Toolchain conflicts receive a selection/flags hint; they are
excluded from the automatic missing-package command.

The version checks follow PHP's configure requirements. Examples include libcurl
7.29.0 for PHP 8.3 and 7.61.0 for PHP 8.4+, libxml 2.9.4/OpenSSL 1.1.1/zlib 1.2.11
for PHP 8.4+, SQLite 3.7.17 and ICU 57.1 for PHP 8.5. Known unsupported libzip
versions 1.3.1 and 1.7.0 are rejected. Sources: [PHP cURL requirements](https://www.php.net/manual/en/curl.requirements.php),
[PHP 8.3 configure macros](https://github.com/php/php-src/blob/PHP-8.3/build/php.m4),
[PHP 8.3 cURL configure](https://github.com/php/php-src/blob/PHP-8.3/ext/curl/config.m4),
[PHP 8.5 configure macros](https://github.com/php/php-src/blob/PHP-8.5/build/php.m4),
[ZIP configure](https://github.com/php/php-src/blob/PHP-8.5/ext/zip/config.m4).

Explicit library `*_CFLAGS`/`*_LIBS` pairs (for example `CURL_CFLAGS` and
`CURL_LIBS`) are honored as PHP configure does: the link probe uses them and
reports that pkg-config version metadata was bypassed. For older PHP releases,
not-yet-built private OpenSSL/curl are shown as **DEFERRED**; PHP configure checks
them after phvm builds them. Doctor does not build or download those libraries.

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
| `PHVM_PROFILE` | `general.default_profile` | `common` |
| `PHVM_JOBS` | `general.parallel_jobs` | `0` (builder default) |
| `PHVM_COLOR` | `general.color` | `true` |
| `PHVM_MIRROR` | `remote.mirror` | `https://www.php.net` |
| `PHVM_USER_AGENT` | `remote.user_agent` | `phvm/1.0.0` |
| `PHVM_TIMEOUT` | `remote.timeout`, seconds per attempt | `60` |
| `PHVM_RETRIES` | `remote.retries` | `3` |
| `PHVM_GPG` | `verify.gpg` | `true` |
| `PHVM_GPG_FALLBACK_SHA256` | `verify.gpg_fallback_sha256` | `true` |
| `PHVM_CONFIGURE_FLAGS` | `build.default_flags`, JSON array of strings | `[]` |

Empty configuration environment variables are treated as unset. Use `true` or
`false` for booleans (`1`/`0` and Go boolean spellings are also accepted).

## Troubleshooting

### Build fails with missing dependencies

Run `phvm doctor` to identify missing dependencies, then install them using your package manager.

Use `phvm doctor --php 8.5.11 --profile common` to check the intended build.
When an existing library is `UNUSABLE`, inspect the reported compiler, linker,
`.pc` file and flags before installing packages again. The command does not change
your shell or system configuration.

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
