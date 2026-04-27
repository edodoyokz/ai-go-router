#!/bin/bash
set -e

# NusaNexus Router - One-line installer
# Usage: curl -fsSL https://raw.githubusercontent.com/edodoyokz/ai-go-router/main/install.sh | bash
# Or: bash <(curl -fsSL https://raw.githubusercontent.com/edodoyokz/ai-go-router/main/install.sh)

REPO="edodoyokz/ai-go-router"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/router}"
DATA_DIR="${DATA_DIR:-$HOME/.local/share/router}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}ℹ${NC} $*"; }
log_ok() { echo -e "${GREEN}✓${NC} $*"; }
log_warn() { echo -e "${YELLOW}⚠${NC} $*"; }
log_err() { echo -e "${RED}✗${NC} $*"; exit 1; }

# Detect OS and architecture
detect_platform() {
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)
    
    case "$os" in
        linux)
            case "$arch" in
                x86_64) echo "linux-amd64" ;;
                aarch64) echo "linux-arm64" ;;
                *) log_err "Unsupported architecture: $arch" ;;
            esac
            ;;
        darwin)
            case "$arch" in
                x86_64) echo "darwin-amd64" ;;
                arm64) echo "darwin-arm64" ;;
                *) log_err "Unsupported architecture: $arch" ;;
            esac
            ;;
        *)
            log_err "Unsupported OS: $os"
            ;;
    esac
}

# Get latest release version
get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
    if [ -z "$version" ]; then
        log_err "Could not fetch latest version from GitHub"
    fi
    echo "$version"
}

# Download and install binary
install_binary() {
    local platform="$1"
    local version="$2"
    local binary_name="router-${platform}"
    local download_url="https://github.com/$REPO/releases/download/$version/$binary_name"
    local temp_file="/tmp/router-install-$$"
    
    log_info "Downloading router $version for $platform..."
    
    if ! curl -fsSL -o "$temp_file" "$download_url"; then
        log_err "Failed to download from $download_url"
    fi
    
    mkdir -p "$INSTALL_DIR"
    chmod +x "$temp_file"
    mv "$temp_file" "$INSTALL_DIR/router"
    
    log_ok "Binary installed to $INSTALL_DIR/router"
}

# Create config directory and example config
setup_config() {
    mkdir -p "$CONFIG_DIR" "$DATA_DIR"
    
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        log_info "Creating example config at $CONFIG_DIR/config.yaml"
        cat > "$CONFIG_DIR/config.yaml" << 'EOF'
server:
  host: 127.0.0.1
  port: 20128
  api_key: sk_router_local_dev

logging:
  level: info
  json_mode: false
  retention_days: 14

storage:
  sqlite_path: ~/.local/share/router/router.db

providers:
  - name: anthropic
    type: anthropic
    format: claude
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    enabled: false
    tier: primary

  - name: openai_compat
    type: openai_compat
    format: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: false
    tier: secondary

model_aliases:
  fast:
    provider: openai_compat
    model: gpt-4.1-mini
  smart:
    provider: anthropic
    model: claude-sonnet-4-5

routes:
  default:
    strategy: fallback
    targets:
      - provider: anthropic
        model: claude-sonnet-4-5
        tier: primary
      - provider: openai_compat
        model: gpt-4.1-mini
        tier: secondary
EOF
        log_ok "Config created at $CONFIG_DIR/config.yaml"
        log_warn "Edit config.yaml and set ANTHROPIC_API_KEY or OPENAI_API_KEY before running"
    else
        log_info "Config already exists at $CONFIG_DIR/config.yaml"
    fi
}

# Create systemd service (Linux only)
setup_systemd() {
    if [ "$(uname -s)" != "Linux" ]; then
        return
    fi
    
    local service_dir="$HOME/.config/systemd/user"
    mkdir -p "$service_dir"
    
    if [ ! -f "$service_dir/router.service" ]; then
        log_info "Creating systemd user service..."
        cat > "$service_dir/router.service" << EOF
[Unit]
Description=NusaNexus Router - AI Model Router
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/router serve --config $CONFIG_DIR/config.yaml
Restart=on-failure
RestartSec=5s

# Environment
Environment="PATH=$INSTALL_DIR:\$PATH"

[Install]
WantedBy=default.target
EOF
        
        systemctl --user daemon-reload 2>/dev/null || true
        log_ok "Systemd service created at $service_dir/router.service"
        log_info "To enable: systemctl --user enable router"
        log_info "To start: systemctl --user start router"
    fi
}

# Verify installation
verify_install() {
    if ! command -v "$INSTALL_DIR/router" &> /dev/null; then
        log_err "Installation verification failed"
    fi
    
    local version=$("$INSTALL_DIR/router" version 2>/dev/null || echo "unknown")
    log_ok "Router installed successfully: $version"
}

# Print next steps
print_next_steps() {
    cat << EOF

${GREEN}✓ Installation complete!${NC}

${BLUE}Next steps:${NC}

1. ${YELLOW}Configure providers:${NC}
   Edit $CONFIG_DIR/config.yaml
   Set ANTHROPIC_API_KEY or OPENAI_API_KEY environment variables

2. ${YELLOW}Test the installation:${NC}
   export ANTHROPIC_API_KEY="your-key-here"
   $INSTALL_DIR/router validate --config $CONFIG_DIR/config.yaml

3. ${YELLOW}Run the server:${NC}
   $INSTALL_DIR/router serve --config $CONFIG_DIR/config.yaml

4. ${YELLOW}Test the endpoint:${NC}
   curl http://127.0.0.1:20128/healthz

5. ${YELLOW}Auto-configure tools (optional):${NC}
   $INSTALL_DIR/router setup --config $CONFIG_DIR/config.yaml

${BLUE}Documentation:${NC}
   https://github.com/$REPO
   https://github.com/$REPO/blob/main/docs/deployment.md

${BLUE}Support:${NC}
   Issues: https://github.com/$REPO/issues
   Discussions: https://github.com/$REPO/discussions

EOF
}

# Main
main() {
    log_info "NusaNexus Router Installer"
    log_info "Repository: $REPO"
    
    local platform version
    platform=$(detect_platform)
    log_ok "Detected platform: $platform"
    
    version=$(get_latest_version)
    log_ok "Latest version: $version"
    
    install_binary "$platform" "$version"
    setup_config
    setup_systemd
    verify_install
    print_next_steps
}

main "$@"
