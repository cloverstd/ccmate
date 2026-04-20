#!/usr/bin/env bash
#
# ccmate installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cloverstd/ccmate/main/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v1.0.0 --systemd
#

set -euo pipefail

# Script-scoped tmpdir so the EXIT trap can reference it safely after the
# install function returns (set -u would otherwise trip on a local variable).
TMPDIR_CCMATE=""
cleanup_tmp() { [[ -n "$TMPDIR_CCMATE" ]] && rm -rf "$TMPDIR_CCMATE"; }
trap cleanup_tmp EXIT

REPO="cloverstd/ccmate"
INSTALL_DIR="/usr/local/bin"
VERSION=""
BINARY_NAME="ccmate"
SETUP_SYSTEMD=false
SERVICE_USER=""
CONFIG_DIR="/etc/ccmate"
DATA_DIR="/var/lib/ccmate"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version|-v) VERSION="$2"; shift 2 ;;
    --dir|-d) INSTALL_DIR="$2"; shift 2 ;;
    --systemd) SETUP_SYSTEMD=true; shift ;;
    --user|-u) SERVICE_USER="$2"; shift 2 ;;
    --config-dir) CONFIG_DIR="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --help|-h)
      cat <<'HELP'
Usage: install.sh [OPTIONS]

Options:
  --version, -v VERSION   Version to install (default: latest stable, 'dev' for pre-release)
  --dir, -d DIR           Binary install directory (default: /usr/local/bin)
  --systemd               Set up systemd service (Linux only)
  --user, -u USER         User to run the service as (default: current user)
                          The user must have claude/codex credentials in their HOME.
  --config-dir DIR        Config directory (default: /etc/ccmate)
  --data-dir DIR          Data directory (default: /var/lib/ccmate)
  --help, -h              Show this help

Examples:
  # Install latest stable
  curl -fsSL https://raw.githubusercontent.com/cloverstd/ccmate/main/install.sh | bash

  # Install with systemd service (runs as current user)
  curl -fsSL ... | bash -s -- --systemd

  # Install as specific user who has claude/codex auth
  curl -fsSL ... | bash -s -- --systemd --user deploy
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
  local url

  url="https://github.com/${REPO}/releases/download/${version}/ccmate-${platform}.tar.gz"
  TMPDIR_CCMATE="$(mktemp -d)"

  echo "Downloading ${url}..."
  if ! curl -fsSL "$url" -o "${TMPDIR_CCMATE}/ccmate.tar.gz"; then
    echo "Error: Failed to download. Check version '${version}' for platform '${platform}'." >&2
    exit 1
  fi

  tar xzf "${TMPDIR_CCMATE}/ccmate.tar.gz" -C "$TMPDIR_CCMATE"

  if [[ -w "$INSTALL_DIR" ]]; then
    mv "${TMPDIR_CCMATE}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    sudo mv "${TMPDIR_CCMATE}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
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

  # Determine service user: explicit --user, or current user
  local svc_user="${SERVICE_USER:-$(whoami)}"
  local svc_group
  svc_group="$(id -gn "$svc_user" 2>/dev/null || echo "$svc_user")"
  local svc_home
  svc_home="$(eval echo "~${svc_user}")"

  if ! id "$svc_user" &>/dev/null; then
    echo "Error: User '${svc_user}' does not exist." >&2
    echo "The service user needs a real HOME with claude/codex credentials." >&2
    exit 1
  fi

  echo "Setting up systemd service..."
  echo "  Service user: ${svc_user}"
  echo "  User HOME:    ${svc_home}"

  # Create directories
  sudo mkdir -p "$CONFIG_DIR" "$DATA_DIR"
  sudo chown "${svc_user}:${svc_group}" "$DATA_DIR"

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
    sudo chown "${svc_user}:${svc_group}" "${CONFIG_DIR}/config.yaml"
    echo "Created default config at ${CONFIG_DIR}/config.yaml"
  else
    echo "Config already exists at ${CONFIG_DIR}/config.yaml, skipping."
  fi

  # Write systemd unit file
  # The service runs as the specified user so it can access:
  #   - Agent CLI tools (claude, codex) in the user's PATH
  #   - Agent auth credentials in the user's HOME (~/.claude, ~/.codex, etc.)
  #   - Git config (~/.gitconfig)
  sudo tee /etc/systemd/system/ccmate.service > /dev/null <<UNIT
[Unit]
Description=ccmate - Coding Agent Manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${svc_user}
Group=${svc_group}
ExecStart=${INSTALL_DIR}/${BINARY_NAME} -config ${CONFIG_DIR}/config.yaml
WorkingDirectory=${DATA_DIR}
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Use the real user HOME so agent CLIs can find their credentials
Environment=HOME=${svc_home}
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${svc_home}/.local/bin:${svc_home}/.npm-global/bin

# Security hardening. Agent CLIs (claude, codex) read credentials from HOME and may
# write session state/cache there, so HOME is RW. Config is RO.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${DATA_DIR} ${svc_home}
ReadOnlyPaths=${CONFIG_DIR}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT

  sudo systemctl daemon-reload
  echo "Systemd service installed."

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
  echo ""
  echo "NOTE: The service runs as '${svc_user}'. Make sure this user has:"
  echo "  - Claude Code / Codex CLI installed and authenticated"
  echo "  - Git configured (git config user.name / user.email)"
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
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --systemd"
  fi

  echo ""
  echo "ccmate ${version} installed successfully."
}

main
