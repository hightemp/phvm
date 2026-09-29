#!/bin/bash
# phvm installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.sh | bash

set -euo pipefail

PHVM_VERSION="${PHVM_VERSION:-latest}"
PHVM_INSTALL_DIR="${PHVM_DIR:-$HOME/.phvm}"
PHVM_BIN_DIR="$PHVM_INSTALL_DIR/bin"
GITHUB_REPO="hightemp/phvm"
INSTALL_TEMP=""
trap 'if [ -n "$INSTALL_TEMP" ]; then rm -rf -- "$INSTALL_TEMP"; fi' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}→${NC} $1"
}

warn() {
    echo -e "${YELLOW}!${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

# Detect OS and architecture
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux*)  OS="linux" ;;
        darwin*) OS="darwin" ;;
        *)       error "Unsupported operating system: $OS" ;;
    esac

    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        amd64)   ARCH="amd64" ;;
        arm64)   ARCH="arm64" ;;
        aarch64) ARCH="arm64" ;;
        *)       error "Unsupported architecture: $ARCH" ;;
    esac

    echo "${OS}_${ARCH}"
}

# Get the latest version from GitHub
get_latest_version() {
    local metadata result
    metadata=$(mktemp)
    if ! download "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" "$metadata"; then
        rm -f "$metadata"
        return 1
    fi
    result=$(grep '"tag_name":' "$metadata" | sed -E 's/.*"([^"]+)".*/\1/') || result=""
    rm -f "$metadata"
    [ -n "$result" ] || return 1
    printf '%s\n' "$result"
}

# Download file
download() {
    local url="$1"
    local dest="$2"

    if [ "$DOWNLOAD_TOOL" = curl ]; then
        curl --proto '=https' --proto-redir '=https' -fsSL "$url" -o "$dest"
    else
        download_wget "$url" "$dest"
    fi
}

# Wget's --https-only applies to recursive links, not ordinary redirects.
# Disable automatic redirects and check every next URL before requesting it.
download_wget() {
    local url="$1" dest="$2" headers status location authority base attempt
    local downloaded=false
    headers=$(mktemp)
    for ((attempt=0; attempt<10; attempt++)); do
        [[ "$url" = https://* ]] && [[ ! "$url" =~ [[:space:]] ]] || break
        authority=${url#https://}; authority=${authority%%/*}
        authority=${authority%%\?*}; authority=${authority%%\#*}
        [ -n "$authority" ] && [[ "$authority" != *@* ]] || break
        if wget --server-response --max-redirect=0 --tries=1 --timeout=60 -O "$dest" -- "$url" 2>"$headers"; then
            downloaded=true
            break
        fi
        status=$(awk '$1 ~ /^HTTP\// {code=$2} END {print code}' "$headers")
        case "$status" in 301|302|303|307|308) ;; *) break ;; esac
        location=$(awk '
            $1 ~ /^HTTP\// {value=""}
            /^[ \t]+[Ll][Oo][Cc][Aa][Tt][Ii][Oo][Nn]:/ {
                value=$0; sub(/^[ \t]*[^:]+:[ \t]*/, "", value); sub(/\r$/, "", value)
            }
            END {print value}' "$headers")
        [ -n "$location" ] && [[ ! "$location" =~ [[:space:]] ]] || break
        case "$location" in
            https://*) url="$location" ;;
            //*) url="https:$location" ;;
            *:*|*\\*) break ;;
            /*) url="https://$authority$location" ;;
            \?*) url="${url%%\?*}$location" ;;
            *)
                base=${url%%\?*}; base=${base%%\#*}
                if [[ "${base#https://}" = */* ]]; then base=${base%/*}; fi
                url="$base/$location"
                ;;
        esac
    done
    rm -f "$headers"
    [ "$downloaded" = true ]
}

verify_archive() {
    local hash filename extra expected="" matches=0 line actual
    while IFS= read -r line || [ -n "$line" ]; do
        line=${line%$'\r'}
        read -r hash filename extra <<< "$line"
        filename=${filename#\*}
        if [ "$filename" = "$ASSET_NAME" ]; then
            [[ "$hash" =~ ^[[:xdigit:]]{64}$ ]] && [ -z "$extra" ] || error "Invalid SHA256 entry for $ASSET_NAME"
            expected=$(printf '%s' "$hash" | tr '[:upper:]' '[:lower:]')
            matches=$((matches + 1))
        fi
    done < "$INSTALL_TEMP/checksums.txt"
    [ "$matches" -eq 1 ] || error "Expected exactly one checksum for $ASSET_NAME"
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$INSTALL_TEMP/archive.tar.gz"); actual=${actual%% *}
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$INSTALL_TEMP/archive.tar.gz"); actual=${actual%% *}
    elif command -v openssl >/dev/null 2>&1; then
        actual=$(openssl dgst -sha256 "$INSTALL_TEMP/archive.tar.gz"); actual=${actual##* }
    else
        error "sha256sum, shasum or openssl is required"
    fi
    [ "$actual" = "$expected" ] || error "SHA256 mismatch for $ASSET_NAME"
}

# Main installation
main() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOAD_TOOL=curl
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOAD_TOOL=wget
    else
        error "curl or wget is required"
    fi
    echo ""
    echo "  ██████╗ ██╗  ██╗██╗   ██╗███╗   ███╗"
    echo "  ██╔══██╗██║  ██║██║   ██║████╗ ████║"
    echo "  ██████╔╝███████║██║   ██║██╔████╔██║"
    echo "  ██╔═══╝ ██╔══██║╚██╗ ██╔╝██║╚██╔╝██║"
    echo "  ██║     ██║  ██║ ╚████╔╝ ██║ ╚═╝ ██║"
    echo "  ╚═╝     ╚═╝  ╚═╝  ╚═══╝  ╚═╝     ╚═╝"
    echo ""
    echo "  PHP Version Manager"
    echo ""

    # Detect platform
    PLATFORM=$(detect_platform)
    info "Detected platform: $PLATFORM"

    # Get version
    if [ "$PHVM_VERSION" = "latest" ]; then
        info "Fetching latest version..."
        PHVM_VERSION=$(get_latest_version)
        if [ -z "$PHVM_VERSION" ]; then
            error "Failed to get latest version"
        fi
    fi
    info "Installing phvm $PHVM_VERSION"
    [[ "$PHVM_VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][a-zA-Z0-9.-]+)?$ ]] || error "Invalid release version"

    # Create directories
    mkdir -p "$PHVM_BIN_DIR"
    [ ! -L "$PHVM_BIN_DIR/phvm" ] || error "Refusing a symlinked destination binary"

    # Download binary
    ASSET_NAME="phvm_${PHVM_VERSION#v}_${PLATFORM}.tar.gz"
    RELEASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${PHVM_VERSION}"
    DOWNLOAD_URL="$RELEASE_URL/$ASSET_NAME"
    INSTALL_TEMP=$(mktemp -d "$PHVM_BIN_DIR/.install-XXXXXXXX")

    info "Downloading from $DOWNLOAD_URL"
    download "$DOWNLOAD_URL" "$INSTALL_TEMP/archive.tar.gz" || error "Failed to download phvm"
    download "$RELEASE_URL/checksums.txt" "$INSTALL_TEMP/checksums.txt" || error "Failed to download checksums"
    verify_archive
    success "SHA256 verified"

    # Extract
    info "Extracting..."
    tar -tzf "$INSTALL_TEMP/archive.tar.gz" > "$INSTALL_TEMP/entries"
    [ "$(awk '$0 == "phvm" {n++} END {print n+0}' "$INSTALL_TEMP/entries")" -eq 1 ] || error "Expected exactly one phvm binary"
    mkdir "$INSTALL_TEMP/extracted"
    tar -xzf "$INSTALL_TEMP/archive.tar.gz" -C "$INSTALL_TEMP/extracted" -- phvm
    [ -f "$INSTALL_TEMP/extracted/phvm" ] && [ ! -L "$INSTALL_TEMP/extracted/phvm" ] || error "Archive binary must be a regular file"
    chmod 755 "$INSTALL_TEMP/extracted/phvm"
    mv -f "$INSTALL_TEMP/extracted/phvm" "$PHVM_BIN_DIR/phvm"

    # Verify
    if [ ! -x "$PHVM_BIN_DIR/phvm" ]; then
        error "Installation failed: binary not found"
    fi

    success "phvm installed to $PHVM_BIN_DIR/phvm"

    # Create directory structure
    info "Creating directory structure..."
    mkdir -p "$PHVM_INSTALL_DIR"/{versions/php,alias,cache/downloads,config,logs}

    # Detect shell
    SHELL_NAME=$(basename "$SHELL")
    PROFILE_FILE=""

    case "$SHELL_NAME" in
        bash)
            if [ -f "$HOME/.bashrc" ]; then
                PROFILE_FILE="$HOME/.bashrc"
            elif [ -f "$HOME/.bash_profile" ]; then
                PROFILE_FILE="$HOME/.bash_profile"
            fi
            ;;
        zsh)
            PROFILE_FILE="$HOME/.zshrc"
            ;;
        fish)
            PROFILE_FILE="$HOME/.config/fish/config.fish"
            ;;
    esac

    echo ""
    success "Installation complete!"
    echo ""

    if [ -n "$PROFILE_FILE" ]; then
        echo "Add the following to $PROFILE_FILE:"
        echo ""
        case "$SHELL_NAME" in
            bash|zsh)
                echo "    eval \"\$($PHVM_BIN_DIR/phvm init $SHELL_NAME)\""
                ;;
            fish)
                echo "    $PHVM_BIN_DIR/phvm init fish | source"
                ;;
        esac
        echo ""
        echo "Then restart your shell or run:"
        echo ""
        echo "    source $PROFILE_FILE"
    else
        echo "Add $PHVM_BIN_DIR to your PATH"
        echo ""
        echo "Run 'phvm init <shell>' to get shell initialization script"
    fi

    echo ""
    echo "Get started:"
    echo ""
    echo "    phvm doctor          # Check build dependencies"
    echo "    phvm ls-remote       # List available versions"
    echo "    phvm install 8.3     # Install PHP 8.3"
    echo ""
}

main "$@"
