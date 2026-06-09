#!/bin/sh
# DeckDex quick installer + first-run helper.
#
#   curl -fsSL https://raw.githubusercontent.com/nobodys-tools/DeckDex/main/install.sh | sh
#   # or: sh install.sh
#
# It downloads the right prebuilt binary for your system from the latest
# release, runs `deckdex detect` so you can confirm it found the correct Steam
# install/account, optionally takes a Steam Web API key, then runs
# `deckdex sync --preset per-tier`.
#
# Env overrides: DECKDEX_INSTALL_DIR (default ~/.local/bin),
#                DECKDEX_REPO (default nobodys-tools/DeckDex).
set -eu

REPO="${DECKDEX_REPO:-nobodys-tools/DeckDex}"
INSTALL_DIR="${DECKDEX_INSTALL_DIR:-$HOME/.local/bin}"

err() { printf '%s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl || err "curl is required."

# --- detect platform -------------------------------------------------------
os="$(uname -s)"
case "$os" in
	Linux)  os=linux ;;
	Darwin) os=darwin ;;
	MINGW*|MSYS*|CYGWIN*) os=windows ;;
	*) err "unsupported OS: $os" ;;
esac
arch="$(uname -m)"
case "$arch" in
	x86_64|amd64) arch=amd64 ;;
	aarch64|arm64) arch=arm64 ;;
	*) err "unsupported architecture: $arch" ;;
esac
ext=""
[ "$os" = "windows" ] && ext=".exe"
asset="deckdex-${os}-${arch}${ext}"

# GitHub serves the newest release's asset from this stable redirect.
url="https://github.com/$REPO/releases/latest/download/$asset"
mkdir -p "$INSTALL_DIR"
bin="$INSTALL_DIR/deckdex${ext}"

printf 'Downloading %s (latest release)...\n' "$asset"
curl -fSL --progress-bar "$url" -o "$bin" || err "download failed: $url"
chmod +x "$bin"
printf 'Installed to %s\n\n' "$bin"

case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) printf 'Note: %s is not on your PATH; add it to run "deckdex" directly later.\n\n' "$INSTALL_DIR" ;;
esac

# --- detect Steam ----------------------------------------------------------
printf '== deckdex detect ==\n'
"$bin" detect || err "detection failed — set [steam].path or --steam-path and re-run."

printf '\nIs the Steam install and account above correct? [y/N] '
read -r ok
case "$ok" in
	y|Y|yes|YES) ;;
	*) err "Aborted. Re-run with --steam-path / --account-id once the right install is set." ;;
esac

# --- optional Steam Web API key --------------------------------------------
printf '\nA free Steam Web API key fetches your FULL library (https://steamcommunity.com/dev/apikey).\n'
printf 'Paste a key for the full library, or press Enter to use local installed games only:\n'
printf 'Steam Web API key (optional): '
stty -echo 2>/dev/null || true
read -r key || true
stty echo 2>/dev/null || true
printf '\n'

# --- sync ------------------------------------------------------------------
printf '\nRunning: deckdex sync --preset per-tier%s\n\n' "$( [ -n "${key:-}" ] && printf ' --api-key <hidden>' )"
if [ -n "${key:-}" ]; then
	"$bin" sync --preset per-tier --api-key "$key"
else
	"$bin" sync --preset per-tier
fi
