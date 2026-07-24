#!/usr/bin/env bash
# install-cli.sh — download and install a pinned linkedin-jobs CLI binary from
# the project's GitHub releases, verifying it against the release's
# checksums.txt before executing it.
#
# Invoked by the linkedin-jobs Hermes skill when `linkedin-jobs` is not on PATH.
#
# VERSION is pinned to a specific release tag (and is bumped automatically by
# release-please on each release — see release-please-config.json extra-files).
# Do not change it to "latest": a pinned tag + checksum verification is what
# makes the download verifiable rather than a blind execute of whatever is
# currently published.
#
# Usage:  bash install-cli.sh
set -euo pipefail

REPO="paputechxyz/linkedin-job-cli"
BINARY_NAME="linkedin-jobs"
VERSION="0.2.0"
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

echo "-> platform: ${goos}/${goarch}  (asset: ${asset}, version: ${VERSION})"

base_url="https://github.com/${REPO}/releases/download/v${VERSION}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# --- download the binary for this platform ---
echo "-> downloading ${base_url}/${asset}"
if ! curl -fL --progress-bar -o "${tmp}/${asset}" "${base_url}/${asset}"; then
    echo "-> download failed. Asset '${asset}' was not found in release v${VERSION}." >&2
    echo "   Check available assets at https://github.com/${REPO}/releases/tag/v${VERSION}" >&2
    echo "   (a release may not exist yet — the maintainer must merge a release-please PR)." >&2
    exit 1
fi

# --- resolve a sha256 tool (macOS ships shasum; Linux/Git-Bash ship sha256sum) ---
sha256_cmd=""
if command -v sha256sum >/dev/null 2>&1; then
    sha256_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    sha256_cmd="shasum -a 256"
else
    echo "-> cannot verify checksum: neither sha256sum nor shasum is installed" >&2
    echo "   refusing to run an unverified binary" >&2
    exit 1
fi

# --- download checksums.txt for this release and verify the binary ---
echo "-> downloading ${base_url}/checksums.txt"
if ! curl -fsSL -o "${tmp}/checksums.txt" "${base_url}/checksums.txt"; then
    echo "-> checksums.txt missing for release v${VERSION}; cannot verify the binary." >&2
    echo "   refusing to run an unverified binary" >&2
    exit 1
fi

expected=$(awk -v a="$asset" '$2==a {print $1; exit}' "${tmp}/checksums.txt")
if [ -z "$expected" ]; then
    echo "-> no checksum entry for '${asset}' in checksums.txt; cannot verify." >&2
    exit 1
fi

actual=$(${sha256_cmd} "${tmp}/${asset}" | awk '{print $1}')
if [ "$expected" != "$actual" ]; then
    echo "-> CHECKSUM MISMATCH for ${asset} — do not run this binary." >&2
    echo "   expected: ${expected}" >&2
    echo "   actual:   ${actual}" >&2
    exit 1
fi
echo "-> verified (sha256 matches checksums.txt)"

chmod +x "${tmp}/${asset}"

# --- install ---
mkdir -p "$INSTALL_DIR"
target="${INSTALL_DIR}/${BINARY_NAME}${ext}"
mv "${tmp}/${asset}" "$target"
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
