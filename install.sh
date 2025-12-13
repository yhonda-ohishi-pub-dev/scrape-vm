#!/bin/bash
# ETC Scraper Installer for Windows (Git Bash / MSYS2)
# Usage: curl -fsSL https://raw.githubusercontent.com/yhonda-ohishi-pub-dev/scrape-vm/main/install.sh | bash

set -e

REPO="yhonda-ohishi-pub-dev/scrape-vm"
INSTALL_DIR="${INSTALL_DIR:-$HOME/bin}"
BINARY_NAME="etc-scraper.exe"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ETC Scraper Installer ===${NC}"

# Check OS
if [[ "$OSTYPE" != "msys" && "$OSTYPE" != "cygwin" && "$OSTYPE" != "win32" ]]; then
    echo -e "${YELLOW}Warning: This installer is designed for Windows (Git Bash/MSYS2)${NC}"
fi

# Get latest release tag
echo "Fetching latest release..."
LATEST_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    echo -e "${RED}Error: Could not fetch latest release${NC}"
    exit 1
fi

echo "Latest version: ${LATEST_TAG}"

# Download URL
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/etc-scraper_${LATEST_TAG}_windows_amd64.zip"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Create temp directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

# Download
echo "Downloading ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/etc-scraper.zip"

# Extract
echo "Extracting..."
unzip -q "$TMP_DIR/etc-scraper.zip" -d "$TMP_DIR"

# Install
echo "Installing to ${INSTALL_DIR}..."
mv "$TMP_DIR/${BINARY_NAME}" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/${BINARY_NAME}"

# Verify
if "$INSTALL_DIR/${BINARY_NAME}" -version > /dev/null 2>&1; then
    echo -e "${GREEN}Successfully installed!${NC}"
    "$INSTALL_DIR/${BINARY_NAME}" -version
else
    echo -e "${RED}Installation failed${NC}"
    exit 1
fi

# Check PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo -e "${YELLOW}Note: Add ${INSTALL_DIR} to your PATH:${NC}"
    echo "  echo 'export PATH=\"\$HOME/bin:\$PATH\"' >> ~/.bashrc"
    echo "  source ~/.bashrc"
fi

echo ""
echo -e "${GREEN}Done! Run 'etc-scraper.exe -help' to get started.${NC}"
