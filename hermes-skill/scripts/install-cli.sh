#!/usr/bin/env bash
# install-cli.sh — download and install the latest linkedin-jobs CLI binary
# from the project's GitHub releases.
#
# Invoked by the linkedin-jobs Hermes skill when `linkedin-jobs` is not on PATH.
# Uses GitHub's /releases/latest/download/<asset> redirect, so it needs no API
# call and is unaffected by unauthenticated rate limits.
#
# Usage:  bash install-cli.sh
set -euo pipefail

REPO="paputechxyz/linkedin-job-cli"
BINARY_NAME="linkedin-jobs"
INSTALL_DIR="${LJ_INSTALL_DIR:-${HOME}/.local/bin}"

# --- detect platform → GOOS/GOARCH (matches the release asset names) ---
case "$(uname -s)" in
    Darwin)               goos="darwin"  ;;
    Linux)                goos="linux"   ;;
    MINGW*|MSYS*|CYGWIN*) goos="windows" ;;
    *) echo "-> unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    arm64|aarch64)   goarch="arm64" ;;
    x86_64|amd64)    goarch="amd64" ;;
    *) echo "-> unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac

ext=""
[ "$goos" = "windows" ] && ext=".exe"
asset="linkedin-jobs_${goos}_${goarch}${ext}"

echo "-> platform: ${goos}/${goarch}  (asset: ${asset})"

# --- download the latest release's matching asset ---
url="https://github.com/${REPO}/releases/latest/download/${asset}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "-> downloading ${url}"
if ! curl -fL --progress-bar -o "${tmp}/${BINARY_NAME}${ext}" "$url"; then
    echo "-> download failed. No release asset named '${asset}' was found." >&2
    echo "   Check available assets at https://github.com/${REPO}/releases/latest" >&2
    echo "   (a release may not exist yet — publish one with 'just release')." >&2
    exit 1
fi
chmod +x "${tmp}/${BINARY_NAME}${ext}"

# --- install ---
mkdir -p "$INSTALL_DIR"
target="${INSTALL_DIR}/${BINARY_NAME}${ext}"
mv "${tmp}/${BINARY_NAME}${ext}" "$target"
echo "-> installed: ${target}"

# --- confirm version + PATH ---
if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    "$BINARY_NAME" version
else
    cat <<EOF

WARNING: ${INSTALL_DIR} is not on your PATH (or this shell hasn't picked it up).
Add it to your shell profile, then start a new shell / session:

    echo 'export PATH="${INSTALL_DIR}:\$PATH"' >> ~/.zshrc    # macOS (zsh)
    echo 'export PATH="${INSTALL_DIR}:\$PATH"' >> ~/.bashrc   # Linux (bash)

The agent cannot run linkedin-jobs until the binary is reachable on PATH.
EOF
    exit 2
fi
