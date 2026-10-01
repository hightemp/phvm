# phvm - PHP Version Manager

[![GitHub release](https://img.shields.io/github/v/release/hightemp/phvm)](https://github.com/hightemp/phvm/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/hightemp/phvm/main?logo=go)](https://github.com/hightemp/phvm/blob/main/go.mod)
[![GitHub downloads](https://img.shields.io/github/downloads/hightemp/phvm/total)](https://github.com/hightemp/phvm/releases)
[![CI](https://github.com/hightemp/phvm/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/hightemp/phvm/actions/workflows/ci.yml)
[![Tests](https://github.com/hightemp/phvm/actions/workflows/scenarios.yml/badge.svg?branch=main)](https://github.com/hightemp/phvm/actions/workflows/scenarios.yml)
[![Release](https://github.com/hightemp/phvm/actions/workflows/release.yml/badge.svg)](https://github.com/hightemp/phvm/actions/workflows/release.yml)
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

Both installers download `checksums.txt` from the selected GitHub release and
require exactly one valid SHA256 entry for the archive. Verification happens
before extraction; only a regular binary is staged and published. Missing or
ambiguous checksums, changed bytes and invalid archives preserve the active
binary. Linux/macOS requires curl or wget and one of sha256sum, shasum or openssl.
Curl is preferred; wget handles a bounded redirect chain explicitly, checking
that each destination uses HTTPS before making the next request.

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

`init` uses the effective phvm root. If `PHVM_DIR` is already set, sourcing the
script preserves it; an explicit `--phvm-dir` selects that root even when the
environment contains another value. For example:

```bash
eval "$(phvm --phvm-dir '/path/with spaces' init bash)"
```

Reloading the script does not duplicate its `PATH` entries. Paths are quoted
for the selected shell. Completions come from the CLI command tree; `use` and
`uninstall` suggest installed versions, while `use` also suggests local aliases.
`install` suggests `latest` and `stable`; enter a numeric branch or release
directly. The generated script configures Zsh completion when needed.

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
| `phvm use [version-or-alias]` | Switch to an explicit or project-selected installed version |
| `phvm current` | Show active version |
| `phvm ls` | List installed versions |
| `phvm ls-remote` | List available versions |

### PHP installation and reinstallation

`phvm install <version> --force` rebuilds an installed version. PHP is configured
with its final prefix and installed into a temporary directory using
[PHP's `INSTALL_ROOT`](https://github.com/php/php-src/blob/PHP-8.3/build/Makefile.global).
The candidate stays outside the installed-version list until publication.

Before publication, phvm checks the PHP version/prefix, CLI runtime, php-config,
module API in the installed headers and startup with the candidate configuration.
Metadata is required and written as part of the candidate. Startup diagnostics
or a metadata write error fail installation. User configuration, Composer
launchers, extension metadata and other existing files are preserved; recorded
extension libraries are copied with their ownership hashes checked. Enabled
extensions must load with the new PHP runtime.

Reinstallation keeps the previous directory until the candidate also passes
validation at its final path. Returned publication errors restore the previous
installation; failures do not switch current or aliases. A transaction journal
allows the next managed-state command to roll back interrupted publication or
clean up a committed transaction.

Directory replacement uses two renames under the shared state lock. Commands
using that lock see the completed state; programs accessing PHP paths directly
can observe the short interval between renames. The journal supports process
interruption recovery and does not promise durability after sudden power loss.
Legacy installations without a publication marker remain supported. New ready
installations require their metadata and SDK files; incomplete or corrupt
publication records are excluded from `ls`.

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

### Project PHP version

Place one version or local alias in `.php-version` and run `phvm use` from the
project directory:

```bash
printf '8.3\n' > .php-version
phvm use
```

Without an argument, `use` reads the nearest `.php-version`, searching from the
current directory through its parents to the filesystem root. It accepts the
same full/partial versions and aliases as `phvm use <version>` and selects only
an installed PHP release. An explicit argument ignores `.php-version`.
`PHVM_VERSION` selects the phvm release in the installer; it does not select a
PHP version for `phvm use`.

The file must be a regular file with one nonempty value (at most 4 KiB).
Symlinks, multiple values, unknown aliases and uninstalled versions produce an
error without changing `current`. No file also produces an error with a hint to
pass a version explicitly. Sourcing shell initialization does not switch PHP
when changing directories; run `phvm use` in the project when needed.

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
| `phvm ext uninstall <name>` | Remove exact configuration, metadata and verified owned library |
| `phvm ext enable <name>` | Enable extension |
| `phvm ext disable <name>` | Disable extension |
| `phvm ext list` | List built-in, enabled, disabled, missing and broken extensions |

Extension identities match exact module or recorded PECL package names, ignoring
case. `redis` never selects `rediscluster`. Loading identity comes from active
`extension` / `zend_extension` directives; an ini filename or comment alone
does not establish ownership. Custom ini names and disabled `.ini.disabled`
files are supported. The provided module from [PECL package.xml](https://pear.php.net/manual/en/guide.developers.package2.pecl.php)
is recorded with its exact binary and ini paths in installation metadata.

```bash
phvm ext list
phvm ext list --php 8.3
phvm ext disable redis --php prod
phvm ext enable redis --php prod
```

`ext list` joins metadata, ini directives, extension-directory files and PHP's
loaded modules. A separate [PHP `-n -m` query](https://www.php.net/manual/en/features.commandline.options.php)
identifies built-in modules. The output distinguishes `builtin`, `enabled`,
`disabled`, `missing` (no available binary) and `broken` (for example an enabled
directive whose module is not loaded). Loaded state and configured enabled
state are tracked separately. External library directories are inspected read-only.

Missing exact identities return an error and a nonzero CLI status. Multiple
loading ini files or an ini shared by multiple extensions block mutation; edit
such files explicitly. Legacy metadata without a module field uses its exact
package name, and does not guess a different module from the ini filename.
Installation requires the exact regular binary produced by the build. Before
publishing it, phvm loads a temporary copy with the selected PHP and checks the
module name and startup diagnostics. Enabled extension dependencies are included
in this isolated check; the working configuration is unchanged. Missing binaries,
wrong module names and incompatible ABIs fail installation.

Uninstall removes the matching ini, metadata entry and library recorded by phvm.
The library must remain inside the selected PHP installation and match its
recorded SHA256. Changed, shared or symlinked libraries block removal. Built-in
modules cannot be uninstalled. Libraries without recorded ownership, including
legacy installations, are retained while their matching ini/metadata are removed.
File writes are atomic and returned write errors trigger rollback. Commands
coordinate through a shared state lock; recovery after process interruption
does not have a transaction guarantee.

### Configuration

| Command | Description |
|---------|-------------|
| `phvm ini get <key>` | Get php.ini value |
| `phvm ini set <key> <value>` | Set php.ini value |
| `phvm ini profile use <name>` | Apply an ini profile to current PHP |
| `phvm ini edit` | Open php.ini in editor |
| `phvm config show` | Show defaults plus TOML settings |
| `phvm config show --effective` | Show settings after environment and explicit CLI overrides |
| `phvm config validate` | Validate TOML, environment and explicit CLI settings |

### Other Commands

| Command | Description |
|---------|-------------|
| `phvm doctor --php 8.5.11 --profile common` | Check tools and libraries for the selected PHP build |
| `phvm composer install [--php <version-or-alias>] [--version X.Y.Z]` | Install the newest compatible or an exact Composer release for one PHP |
| `phvm composer update [--php <version-or-alias>]` | Verify and atomically update Composer for one PHP |
| `phvm cache clear [--downloads|--sources|--build|--all]` | Clear cache while preserving installed Composer |
| `phvm init <shell>` | Print shell init script |

## CLI automation

Results, reports, help and shell initialization scripts go to **stdout**.
Progress, warnings and errors go to **stderr**. Always check the exit status:
a diagnostic report such as `doctor` can contain results even when checks fail.

| Exit code | Meaning |
|-----------|---------|
| `0` | Command completed successfully |
| `1` | Invalid arguments or a configuration, filesystem, network, build or editor error |
| `130` | Interrupted with Ctrl+C / SIGINT |
| `143` | Terminated with SIGTERM on Unix |

`current` checks that its PHP binary exists and is executable on Unix. `which`
accepts one filename in the selected installation's `bin` directory, checks that
it is a regular executable, and prints its absolute path. Missing binaries,
unsafe paths and corrupt current/alias storage return errors.

```bash
php_bin="$(phvm which php)" || exit $?
"$php_bin" --version
```

Interrupting a build cancels its command context, stops its subprocesses, runs
transaction cleanup and releases the state lock so installation can be retried.
On Unix, build and probe tools run in a separate process group; cancellation
kills that group. Children that deliberately detach into another group are
outside this mechanism. On Windows, cancellation uses `taskkill /T /F` with a
bounded wait. Windows has a separate native subprocess test in CI; compilation
on Linux does not verify its runtime behavior or native PHP installation.

`ini open` accepts a quoted executable and arguments in `EDITOR` or `VISUAL`,
for example `EDITOR='code --wait'`. Unix editors keep the terminal's foreground
process group; cancellation stops the editor itself.

## Updating Composer

```bash
phvm composer install                 # Newest compatible release for current PHP
phvm composer install --php 5.6 --version 2.2.30
phvm composer update
phvm composer update --php 8.3
phvm composer update --php prod
```

`--php` selects an installed PHP version/alias; the default is current. Without
`--version`, phvm reads [Composer's official release channels](https://getcomposer.org/download/#download-channels) and selects
the newest release compatible with that PHP. For example, an old PHP 5.6 can
use Composer 2.2 LTS while PHP 8.3 uses the current stable branch. `--version`
accepts an exact `X.Y.Z` release and may intentionally select an older Composer;
the selected PHP must still run it successfully. PHP older than 5.3.2 is not
supported by [Composer 2](https://getcomposer.org/doc/00-intro.md#system-requirements). Phvm downloads the selected PHAR to a unique staging
file, checks its official SHA256, and runs the verified candidate's `--version`
with the selected PHP before replacing the active file atomically. The reported
version must match the requested or selected release. The shell launcher is never
passed to PHP as source. Version probes disable Composer
plugins, scripts, ANSI and interaction using [Composer's global options](https://getcomposer.org/doc/03-cli.md#global-options).

Successful output includes the before/after versions. An identical version is
reported as already up to date; a downgrade is rejected. A failed download,
checksum, PHP runtime/version check or cancellation preserves the active PHAR
and launchers, returns a nonzero CLI status and removes staging files.

Each PHP version has its own PHAR at
`$PHVM_DIR/tools/composer/<php-version>/composer.phar`. Its `bin/composer`
launcher runs that version's PHP with that PHAR. Updating PHP 8.3's Composer
does not change PHP 5.6's Composer. Working PHARs are outside cache cleanup.
`composer disable --php X` removes only X's launcher; its PHAR remains for a
later `composer enable --php X`.

### Composer storage and cache cleanup

Install, enable and update use permanent per-PHP storage. For compatibility,
`composer install --global` downloads a shared seed at
`$PHVM_DIR/tools/composer/composer.phar`; it is not the active PHAR for enabled
PHP versions. `composer enable --php X` copies and validates that seed when X
has no selected PHAR. If the seed is incompatible, install a compatible release
with `composer install --php X` instead.
`cache clear` and `cache clear --all` remove disposable download/source/build and
extension archive files. `cache clear --downloads` removes downloaded PHP files.
These commands preserve Composer. Composer still works after
the entire cache directory is removed once its installation uses the permanent
path.

Before clearing downloads, phvm migrates old shared and cached Composer PHARs
and standard managed launchers. It copies and checks the PHAR with each target
PHP before redirecting that version's launcher. An existing permanent shared
PHAR takes precedence over cached legacy bytes. Migration is repeatable; old
source bytes remain until cleanup succeeds. An incompatible PHP needs an
explicit `composer install --php X` to select a compatible release.

A migration error stops cleanup before deletion. An unrecognized custom launcher
that still references the legacy PHAR is preserved and diagnosed; use
`phvm composer enable --php <version>` to explicitly replace it, then retry
cleanup. Symlink storage/PHAR paths are rejected. Composer launchers quote paths
literally, including spaces, quotes, dollar signs and backticks.

Commands using managed PHP state wait for other operations in the same phvm
directory. This coordinates installation, removal, extension metadata changes,
Composer publication and cache cleanup. Long builds also hold this lock; Ctrl+C
cancels a command waiting for it. Version/help/config output, remote listings
and doctor do not wait for the state lock.

Downloads use unique temporary files and serialize writers and cleanup for each
cache directory. A waiting download rechecks the cache after acquiring the lock.
PHP archives are checked against the expected SHA256 before publication and
before reuse; a damaged cached archive is downloaded again. Failed or cancelled
downloads preserve the previous published file. Lock files remain outside the
cleared cache parts so waiting commands continue to use the same lock.

## Download verification

| Component | Trust and checks before execution |
|-----------|----------------------------------|
| PHP source | Required SHA256 from release metadata; configured GPG verification uses the PHP keyring |
| Private OpenSSL/curl | Required SHA256 pinned in phvm's dependency catalog |
| PECL by default | HTTPS channel metadata, exact package/version, archive size, separately fetched file checksums and safe archive structure |
| PECL with `--sha256` | Required user-pinned archive digest and package/version/structure validation; exact cached releases can be used without channel metadata |
| phvm release binary | Required SHA256 from the selected GitHub release's `checksums.txt` |

Downloads are checked in staging and again before cache reuse. An invalid cache
is discarded and fetched once; failed replacements are not published. HTTP
retry settings still apply to transport failures. HTTPS redirects to HTTP are
rejected. An explicitly configured HTTP PHP mirror continues to use HTTP.

PECL's published file checksums use MD5. They detect disagreement between the
archive and channel metadata; they are **not a signature or a strong independent
trust anchor**. For a fixed digest obtained from a trusted source:

```bash
phvm ext install redis --version 6.0.0 \
  --sha256="${PECL_ARCHIVE_SHA256:?Set the trusted archive SHA256}"
```

Without a pin, installation stops if channel metadata is unavailable, invalid
or inconsistent. Archive checks reject missing/extra files, duplicate entries,
unsafe paths, links and special files. XML manifests are limited to 8 MiB;
archives to 100,000 entries and 1 GiB of decompressed data. Extension metadata
records the archive SHA256 and verification method. An observed digest from
an unpinned download does not become an independent trust anchor.

Private dependency markers record the exact version, source URL, archive digest,
configure command and build dependency fingerprints. Legacy or mismatched
markers trigger rebuilding before PHP compilation; doctor reports these
dependencies as deferred. Pinning source bytes does not update the supported
legacy library versions or attest to the publisher's security.

Release checksums and default PECL metadata rely on the corresponding HTTPS
publisher. Independent release signatures/provenance are not checked by the
bootstrap installers. Run `make test-installers` for isolated installer tests;
PowerShell cases require pwsh and also run in the Windows CI matrix.

## Build Profiles

When installing PHP, you can choose a build profile:

- **minimal** - Core PHP only, smallest footprint
- **common** - Common extensions (curl, json, mbstring, openssl, etc.)
- **full** - A broader set of bundled extensions

```bash
# Install with minimal profile
phvm install 8.3 --profile minimal

# Install with full profile
phvm install 8.3 --profile full
```

Unknown profile names are errors. Built-in profile flags are adapted to the PHP
version; explicit flags using an obsolete spelling are rejected with an error.

### Configure arguments

Use repeatable `--configure-flag` for one exact argument per option. Commas,
spaces and shell-looking text inside its value are retained as data:

```bash
phvm install 8.3 --configure-flag=--without-curl --configure-flag=--without-openssl
phvm ext install redis --configure-flag=--enable-redis
phvm doctor --php 8.3 --profile common --configure-flag=--without-curl
phvm config show --effective --configure-flag='CFLAGS=-O0 -g'
```

Legacy `--configure` accepts a quoted argument string, for example
`--configure='--with-sdk="/path with space" --without-curl'`. Phvm parses quotes
and escapes without invoking a shell. `$HOME`, backticks and `$()` are not
expanded. Use `--name=value` for an option value; empty arguments, dangling
words and unfinished quotes are errors. If both forms are used, repeatable
flags override the legacy string. The last occurrence of the same option wins.

PHP flag priority is **dependency defaults → profile → config/env → explicit
CLI**. A bare enabled library option uses its private dependency prefix when
needed by old PHP. Explicit disabling or a supplied prefix takes priority.
`minimal` does not implicitly enable OpenSSL/cURL. cURL can still require
OpenSSL as a build dependency while PHP's OpenSSL extension is disabled.
An explicit external OpenSSL prefix combined with private cURL is rejected;
also choose a cURL prefix or disable cURL in that case.

PHP and PECL installation check explicit options against the selected source's
`configure --help`. `doctor` applies the same PHP flag plan and known version
rules without downloading source. Managed install/ini paths, the selected PECL
php-config and PHP CLI cannot be overridden. Metadata stores the actual argv
and selected build environment passed to configure for PHP and extensions.

## php.ini Profiles

Switch between development and production configurations:

```bash
# Apply development settings while preserving conf.d
phvm ini profile use development

# Apply production settings while preserving conf.d
phvm ini profile use production

# Save current php.ini and all conf.d as an exact snapshot
phvm ini profile save project

# Restore it and keep the last three managed backups
phvm ini profile use project --backup-keep 3
```

Saved profiles are **snapshots of `php.ini` and the entire `conf.d` directory**.
Saving the same name replaces its previous contents, including removed files.
Applying a snapshot removes configuration files absent from it. Other files in
the PHP `etc` directory, such as FPM configuration, remain unchanged. Enabled
and disabled counterparts cannot coexist in a valid profile.

Built-in development/production profiles change only `php.ini`. Saving your own
snapshot under either name deliberately replaces that default. Legacy profiles
without `profile.json` use snapshot semantics when they contain `conf.d`, and
php.ini-only semantics otherwise. New snapshots record their mode explicitly.

Profile application prepares and validates a temporary configuration using
current PHP before publication, then checks the final path. Syntax/startup
errors stop application. Returned copy/publication errors restore the previous
configuration; rollback errors retain the temporary previous copy for repair.
Exact recorded extension ini/module mappings have their enabled metadata updated.
Libraries and ownership records are retained.

Backups are enabled by default and use unique `etc.backup-<time>-<id>` directories
inside the PHP version. Each includes the old configuration and available version
metadata. `--backup-keep` accepts 1–100, defaults to 5, and prunes only completed
managed backups after a successful apply. `--backup=false` creates no backup.
Legacy `etc.backup` and unrecognized backup directories are preserved. A pruning
failure is reported and may temporarily leave more backups than the limit.

Operations use the shared state lock. Directory publication uses two renames;
direct readers can observe the short interval between them. This profile workflow
does not promise automatic recovery from process death or power loss. Saving
validates file structure; PHP startup validation runs when applying the profile.

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
├── tools/
│   └── composer/
│       ├── 8.3.15/composer.phar  # Composer selected for PHP 8.3.15
│       └── 8.2.27/composer.phar  # Independent Composer for PHP 8.2.27
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
| `general.parallel_jobs` | `install --jobs`, `ext install --jobs` | Nonnegative integer; `0` selects memory-aware automatic jobs; an explicit positive value has priority |
| `general.color` | `--no-color[=false]` | Boolean; `--no-color` disables color, `--no-color=false` enables it |
| `remote.mirror` | `--mirror` | Absolute HTTP(S) base URL without query/fragment; PHP API and source downloads |
| `remote.user_agent` | `--user-agent` | Nonempty, single-line HTTP User-Agent |
| `remote.timeout` | `--timeout` | Integer seconds, 1–86400, for each HTTP attempt |
| `remote.retries` | `--retries` | Integer, 0–10; retries after the initial HTTP attempt |
| `verify.sha256` | None | Must be `true`; SHA256 is mandatory |
| `verify.gpg` | `--gpg`, `install --skip-gpg` | Boolean; PHP signature verification |
| `verify.gpg_fallback_sha256` | `--gpg-fallback-sha256` | Boolean; permit unavailable GPG verification only after successful SHA256 |
| `build.default_flags` | `install/doctor --configure-flag`, legacy `--configure` | Array of complete, single-line PHP configure arguments |

For automatic jobs (`0`), phvm uses available memory as well as CPU count:
it reserves 1 GiB and allows roughly one job per additional 2 GiB. PHP is
also capped at half the logical CPUs; PECL is capped at two jobs. If available
memory cannot be measured, automatic builds use one job. A positive `--jobs`
overrides this heuristic, including when it exceeds the memory-based value.
Memory can change during a build, so this is a starting estimate rather than
a process memory limit.

Environment variables are listed below. `PHVM_CONFIGURE_FLAGS` replaces the
TOML flags array; explicit `--configure`/`--configure-flag` arguments are merged
over it, replacing conflicting options. These settings
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
`config validate` accept `--profile`, `--jobs`, `--configure-flag` and legacy
`--configure` to inspect PHP
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

### Lifecycle scenarios

```bash
# Deterministic HTTP, build-tool and failure fixtures
make test-scenarios

# Real PHP with synthetic PHAR and compiled module fixtures
make test-runtime-scenarios

# Official Composer and PECL redis: download, build, load and uninstall
make test-native-scenarios
```

The native scenarios need Linux, network access, PHP with Phar, matching
`php-config`/`phpize`, `cc`, `make` and `autoconf`. They borrow the existing SDK
and use temporary installation roots. CI provisions the SDK on its runner and
executes all three profiles on every PR and push. Ordinary `make test` runs the
offline regressions and can skip tests whose native prerequisites are absent.

The scenario runner rejects a skipped or missing required test, including
skipped subtests. It writes the results, states and proof levels to
`reports/scenarios-<profile>-<platform>.json`, with the original Go test events
beside it in `.jsonl`. See the [scenario matrix](docs/scenario-matrix.md) for
coverage, platform limits and the separate opt-in PHP source build.

### Security checks

```bash
# Known vulnerabilities in dependencies and the Go standard library
make govulncheck

# Potential security issues in application source code
make gosec

# Run both checks
make security
```

The tools run through `go run` with versions pinned in `scripts/tool_versions.json`:
govulncheck `v1.8.0`, gosec `v2.29.0`, and golangci-lint `v2.14.0`.
Release checks also pin GoReleaser `v2.18.2` and actionlint `v1.7.12`.
No separate global installation is required. The first run downloads the
tools and their dependencies to the Go module cache.

`make gosec` remains strict and returns a nonzero status for any findings.
CI and release preflight use `make security-baseline`: the full report is kept
in `reports/gosec.json`, while new findings block the command. The reviewed
`.gosec-baseline.json` lists the remaining reviewed findings; it does not
declare them fixed. Identity includes rule, file, code, severity and confidence.
Moving source line numbers alone does not add an exception. Missing or invalid
scanner reports and analysis errors fail the check. Refreshing the baseline
requires an explicit review and is never performed by CI.

CI tests run on the minimum Go version declared in `go.mod` and current stable
Go. Release packaging uses `go.mod` and the same pinned tools as local preflight.

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
printf '1.1.0\n' > VERSION
make release
```

Run checks without creating a commit/tag or publishing:

```bash
make release-check
```

This runs version/source checks, dependency verification, vet, lint, race tests,
govulncheck, the gosec baseline, installer/release/checker regressions,
required deterministic lifecycle scenarios,
actionlint, GoReleaser schema validation, and snapshot packaging. The snapshot
uses `VERSION` even before its tag exists. All six archives must match installer
filenames, SHA256, executable format/architecture and embedded Go version flags;
the host binary is extracted and its `version` command executed. Checks also
require LICENSE/README and reject links, duplicates and unsafe archive paths.
Build artifacts stay in ignored `dist/`, reports in ignored `reports/`.

`make release` previews all included changes and runs this preflight before
staging. Failure creates no commit/tag and preserves the existing index. Source,
index, HEAD and VERSION changes during preflight require retrying the checks.
After success, it stages all non-ignored changes, creates a `chore: release vX.Y.Z`
commit and an annotated `vX.Y.Z` tag, then pushes the current branch and tag to
`origin` atomically. The tag push triggers `.github/workflows/release.yml`.
That workflow validates tag/committed VERSION, repeats preflight and runs archive
smoke on Linux/macOS/Windows before its publication job. Foreign binaries receive
build metadata checks locally; CI executes the matching native target on each OS.
Publication uses pinned GoReleaser to rebuild from the checked tag; snapshot
artifacts are validation outputs rather than the final uploaded bytes.
Existing local or remote tags are rejected before committing. If the push
fails, the local commit and tag remain, and the command prints how to retry.
Update `VERSION` before each new release.

Test the automation using temporary local Git repositories:

```bash
make test-release
make test-installers
make test-checks
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

On a failed build, phvm prints the failing stage, one relevant error, a likely
cause and the path to `~/.phvm/logs/install-<version>.log` (or `$PHVM_DIR/logs`).
The terminal output stays short even if `make` emits hundreds of repeated linker
errors. The file retains the complete build-tool output, including private
dependency builds; when `configure` fails, its `config.log` is appended there
so the compiler/linker detail is available in one place. The install log is
created with owner-only permissions and redacts URL credentials, labelled
tokens/passwords and known sensitive environment values. The original
source-generated `config.log` may still contain raw values, so review it before
sharing. If the install log cannot be opened safely, the error says it is
unavailable instead of pointing to a nonexistent full log.

Before extracting PHP or PECL sources, phvm checks the free space on the
temporary/build filesystem and the installation filesystem using the verified
archive size plus a safety allowance; forced PHP reinstalls also account for
the existing installation's files. It checks the installation filesystem
again immediately before publishing PHP or an extension. An insufficient-space
error reports the phase, filesystem path and estimated required/available MiB.
This estimate cannot guarantee that another process will not consume space
while compilation runs.

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
