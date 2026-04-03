#!/usr/bin/env bash
#
# ccmate installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cloverstd/ccmate/master/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v1.0.0 --systemd
#

set -euo pipefail

REPO="cloverstd/ccmate"
INSTALL_DIR="/usr/local/bin"
VERSION=""
BINARY_NAME="ccmate"
SETUP_SYSTEMD=false
CONFIG_DIR="/etc/ccmate"
DATA_DIR="/var/lib/ccmate"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version|-v) VERSION="$2"; shift 2 ;;
    --dir|-d) INSTALL_DIR="$2"; shift 2 ;;
    --systemd) SETUP_SYSTEMD=true; shift ;;
    --config-dir) CONFIG_DIR="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --help|-h)
      cat <<'HELP'
Usage: install.sh [OPTIONS]

Options:
  --version, -v VERSION   Version to install (default: latest stable, 'dev' for pre-release)
  --dir, -d DIR           Binary install directory (default: /usr/local/bin)
  --systemd               Set up systemd service (Linux only)
  --config-dir DIR        Config directory (default: /etc/ccmate)
  --data-dir DIR          Data directory (default: /var/lib/ccmate)
  --help, -h              Show this help

Examples:
  # Install latest stable
  curl -fsSL https://raw.githubusercontent.com/cloverstd/ccmate/master/install.sh | bash

  # Install with systemd service
  curl -fsSL ... | bash -s -- --systemd

  # Install specific version
  curl -fsSL ... | bash -s -- --version v1.0.0 --systemd
HELP
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

detect_platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"

  case "$os" in
    linux)  os="linux" ;;
    darwin) os="darwin" ;;
    *) echo "Unsupported OS: $os"; exit 1 ;;
  esac

  case "$arch" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "Unsupported architecture: $arch"; exit 1 ;;
  esac

  echo "${os}-${arch}"
}

resolve_version() {
  if [[ -n "$VERSION" ]]; then
    if [[ "$VERSION" == "dev" || "$VERSION" == "latest-dev" ]]; then
      VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
        | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')
    fi
    echo "$VERSION"
    return
  fi

  local tag
  tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/') || true

  if [[ -z "$tag" ]]; then
    tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
      | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')
  fi

  if [[ -z "$tag" ]]; then
    echo "Error: No releases found" >&2
    exit 1
  fi

  echo "$tag"
}

install_binary() {
  local platform="$1" version="$2"
  local url tmpdir

  url="https://github.com/${REPO}/releases/download/${version}/ccmate-${platform}.tar.gz"
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  echo "Downloading ${url}..."
  if ! curl -fsSL "$url" -o "${tmpdir}/ccmate.tar.gz"; then
    echo "Error: Failed to download. Check version '${version}' for platform '${platform}'." >&2
    exit 1
  fi

  tar xzf "${tmpdir}/ccmate.tar.gz" -C "$tmpdir"

  if [[ -w "$INSTALL_DIR" ]]; then
    mv "${tmpdir}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    sudo mv "${tmpdir}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
  fi
  chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

  echo "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

setup_systemd() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    echo "Warning: --systemd is only supported on Linux, skipping."
    return
  fi

  if ! command -v systemctl &>/dev/null; then
    echo "Warning: systemctl not found, skipping systemd setup."
    return
  fi

  echo "Setting up systemd service..."

  # Create system user if not exists
  if ! id ccmate &>/dev/null; then
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin ccmate
    echo "Created system user 'ccmate'"
  fi

  # Create directories
  sudo mkdir -p "$CONFIG_DIR" "$DATA_DIR"
  sudo chown ccmate:ccmate "$DATA_DIR"

  # Create default config if not exists
  if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
    sudo tee "${CONFIG_DIR}/config.yaml" > /dev/null <<YAML
server:
  host: "0.0.0.0"
  port: 8080

database:
  driver: sqlite3
  dsn: "${DATA_DIR}/ccmate.db"
YAML
    sudo chown ccmate:ccmate "${CONFIG_DIR}/config.yaml"
    echo "Created default config at ${CONFIG_DIR}/config.yaml"
  else
    echo "Config already exists at ${CONFIG_DIR}/config.yaml, skipping."
  fi

  # Write systemd unit file
  sudo tee /etc/systemd/system/ccmate.service > /dev/null <<UNIT
[Unit]
Description=ccmate - Coding Agent Manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ccmate
Group=ccmate
ExecStart=${INSTALL_DIR}/${BINARY_NAME} -config ${CONFIG_DIR}/config.yaml
WorkingDirectory=${DATA_DIR}
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${DATA_DIR}
ReadOnlyPaths=${CONFIG_DIR}
PrivateTmp=true

# Environment
Environment=HOME=${DATA_DIR}

[Install]
WantedBy=multi-user.target
UNIT

  sudo systemctl daemon-reload
  echo "Systemd service installed."

  # Enable and start if this is a fresh install
  if ! systemctl is-enabled ccmate &>/dev/null; then
    sudo systemctl enable ccmate
    echo "Service enabled on boot."
  fi

  echo ""
  echo "Manage the service:"
  echo "  sudo systemctl start ccmate      # Start"
  echo "  sudo systemctl stop ccmate       # Stop"
  echo "  sudo systemctl restart ccmate    # Restart"
  echo "  sudo systemctl status ccmate     # Status"
  echo "  sudo journalctl -u ccmate -f     # Logs"
  echo ""
  echo "Config: ${CONFIG_DIR}/config.yaml"
  echo "Data:   ${DATA_DIR}/"
}

main() {
  local platform version

  platform="$(detect_platform)"
  version="$(resolve_version)"

  echo "Installing ccmate ${version} for ${platform}..."
  echo ""

  install_binary "$platform" "$version"

  if [[ "$SETUP_SYSTEMD" == true ]]; then
    echo ""
    setup_systemd
  else
    echo ""
    echo "Quick start:"
    echo "  ${BINARY_NAME} -config config.yaml"
    echo ""
    echo "To set up as a systemd service, re-run with --systemd:"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/master/install.sh | bash -s -- --systemd"
  fi

  echo ""
  echo "ccmate ${version} installed successfully."
}

main
