#!/usr/bin/env bash
#
# ccmate installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cloverstd/ccmate/master/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v1.0.0 --dir /usr/local/bin
#

set -euo pipefail

REPO="cloverstd/ccmate"
INSTALL_DIR="/usr/local/bin"
VERSION=""
BINARY_NAME="ccmate"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version|-v) VERSION="$2"; shift 2 ;;
    --dir|-d) INSTALL_DIR="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: install.sh [--version VERSION] [--dir INSTALL_DIR]"
      echo "  --version, -v   Version to install (default: latest stable, or 'dev' for latest pre-release)"
      echo "  --dir, -d       Installation directory (default: /usr/local/bin)"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# Detect OS and architecture
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

# Find the right version to download
resolve_version() {
  if [[ -n "$VERSION" ]]; then
    if [[ "$VERSION" == "dev" || "$VERSION" == "latest-dev" ]]; then
      # Get latest pre-release
      VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
        | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')
    fi
    echo "$VERSION"
    return
  fi

  # Get latest stable release
  local tag
  tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')

  if [[ -z "$tag" ]]; then
    # No stable release, fall back to latest pre-release
    tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
      | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')
  fi

  if [[ -z "$tag" ]]; then
    echo "Error: No releases found" >&2
    exit 1
  fi

  echo "$tag"
}

main() {
  local platform version url tmpdir

  platform="$(detect_platform)"
  version="$(resolve_version)"

  echo "Installing ccmate ${version} for ${platform}..."

  url="https://github.com/${REPO}/releases/download/${version}/ccmate-${platform}.tar.gz"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  echo "Downloading ${url}..."
  if ! curl -fsSL "$url" -o "${tmpdir}/ccmate.tar.gz"; then
    echo "Error: Failed to download. Check version '${version}' exists for platform '${platform}'." >&2
    exit 1
  fi

  tar xzf "${tmpdir}/ccmate.tar.gz" -C "$tmpdir"

  # Install binary
  if [[ -w "$INSTALL_DIR" ]]; then
    mv "${tmpdir}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    echo "Need sudo to install to ${INSTALL_DIR}"
    sudo mv "${tmpdir}/ccmate-${platform}" "${INSTALL_DIR}/${BINARY_NAME}"
  fi
  chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

  echo ""
  echo "ccmate ${version} installed to ${INSTALL_DIR}/${BINARY_NAME}"
  echo ""
  echo "Quick start:"
  echo "  ccmate -config config.yaml"
  echo ""
  echo "Create a config file:"
  echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/master/config.example.yaml -o config.yaml"
}

main
