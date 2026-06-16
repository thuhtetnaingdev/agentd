#!/usr/bin/env bash
# Install agentd — AI-powered DevOps agent
# One-liner: curl -fsSL https://raw.githubusercontent.com/thuhtetnaingdev/agentd/main/install.sh | bash
set -euo pipefail

REPO="thuhtetnaingdev/agentd"
BIN="agentd"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

# --- detect OS/Arch ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: $OS (only macOS and Linux are supported)"
    exit 1
    ;;
esac

# --- resolve version ---
if [ "$VERSION" = "latest" ]; then
  TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  if [ -z "$TAG" ]; then
    echo "Failed to fetch latest version from GitHub."
    exit 1
  fi
else
  TAG="$VERSION"
fi

ASSET="${BIN}_${OS}_${ARCH}"
URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"

echo "→ Installing agentd $TAG for $OS/$ARCH ..."
echo "→ Downloading $URL"

# Download to temp file
TMP=$(mktemp)
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# Install
if [ ! -d "$INSTALL_DIR" ]; then
  echo "→ Creating $INSTALL_DIR"
  mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
fi

if mv "$TMP" "$INSTALL_DIR/$BIN" 2>/dev/null; then
  :
else
  echo "→ Need sudo to install to $INSTALL_DIR"
  sudo mv "$TMP" "$INSTALL_DIR/$BIN"
fi

echo "✓ agentd $TAG installed to $INSTALL_DIR/$BIN"
echo ""
echo "  Run:  agentd --help"
echo "  Update: agentd update"
echo "  Uninstall: agentd uninstall"
