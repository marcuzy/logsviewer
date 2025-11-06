#!/usr/bin/env bash
set -euo pipefail

REPO="marcuzy/logsviewer"
VERSION="${VERSION:-v0.1.0}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="logsviewer"

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }

OS=$(uname -s)
ARCH=$(uname -m)

case "$OS/$ARCH" in
  Darwin/arm64)   ASSET="logsviewer-darwin-arm64" ;;
  Linux/x86_64|Linux/amd64) ASSET="logsviewer-linux-amd64" ;;
  *)
    echo "Unsupported platform: ${OS} ${ARCH}" >&2
    exit 1
    ;;
esac

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ASSET_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
TARGET="${TMPDIR}/${BINARY_NAME}"

echo "Downloading ${ASSET_URL}"
curl -fsSL "${ASSET_URL}" -o "${TARGET}"

chmod +x "${TARGET}"

echo "Installing to ${INSTALL_DIR}/${BINARY_NAME}"
install -m 0755 "${TARGET}" "${INSTALL_DIR}/${BINARY_NAME}"

echo "Installed ${BINARY_NAME} ${VERSION} -> ${INSTALL_DIR}/${BINARY_NAME}"
echo "Run: ${BINARY_NAME} --help"
