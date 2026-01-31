#!/bin/bash
# phvm installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/hightemp/phvm/main/scripts/install.sh | bash

set -e

PHVM_VERSION="${PHVM_VERSION:-latest}"
PHVM_INSTALL_DIR="${PHVM_DIR:-$HOME/.phvm}"
PHVM_BIN_DIR="$PHVM_INSTALL_DIR/bin"
GITHUB_REPO="hightemp/phvm"

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
    if command -v curl &> /dev/null; then
        curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
            grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
    elif command -v wget &> /dev/null; then
        wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
            grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
    else
        error "curl or wget is required"
    fi
}

# Download file
download() {
    local url="$1"
    local dest="$2"

    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget &> /dev/null; then
        wget -q "$url" -O "$dest"
    else
        error "curl or wget is required"
    fi
}

# Main installation
main() {
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

    # Create directories
    mkdir -p "$PHVM_BIN_DIR"

    # Download binary
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${PHVM_VERSION}/phvm_${PHVM_VERSION#v}_${PLATFORM}.tar.gz"
    TEMP_FILE=$(mktemp)

    info "Downloading from $DOWNLOAD_URL"
    download "$DOWNLOAD_URL" "$TEMP_FILE" || error "Failed to download phvm"

    # Extract
    info "Extracting..."
    tar -xzf "$TEMP_FILE" -C "$PHVM_BIN_DIR" phvm
    chmod +x "$PHVM_BIN_DIR/phvm"
    rm -f "$TEMP_FILE"

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
