#!/bin/sh
# DeckDex uninstaller — removes the binary, cache, config, state and backups.
#
#   curl -fsSL https://raw.githubusercontent.com/nobodys-tools/DeckDex/main/uninstall.sh | sh
#   # or: sh uninstall.sh [-y]
#
# It first OFFERS to remove the Steam collections DeckDex created (via
# `deckdex prune`) so they aren't orphaned, then deletes all local files.
#
# Flags/env: -y / --yes           skip prompts (also skips pruning collections)
#            DECKDEX_INSTALL_DIR  where the binary was installed (default ~/.local/bin)
set -eu

YES=0
case "${1:-}" in -y|--yes) YES=1 ;; esac

INSTALL_DIR="${DECKDEX_INSTALL_DIR:-$HOME/.local/bin}"

# Resolve the per-OS data locations the same way the binary does.
case "$(uname -s)" in
	Darwin)
		CONFIG="$HOME/Library/Application Support/deckdex"
		CACHE="$HOME/Library/Caches/deckdex"
		;;
	*)
		CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/deckdex"
		CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/deckdex"
		;;
esac
# State (and backups) live under XDG_STATE_HOME if set, else inside CONFIG.
if [ -n "${XDG_STATE_HOME:-}" ]; then STATE="$XDG_STATE_HOME/deckdex"; else STATE="$CONFIG/state"; fi

# Locate the binary (configured dir first, then PATH).
bin=""
if [ -x "$INSTALL_DIR/deckdex" ]; then bin="$INSTALL_DIR/deckdex"
elif command -v deckdex >/dev/null 2>&1; then bin="$(command -v deckdex)"; fi

prompt() { # prompt MESSAGE -> 0 if yes
	[ "$YES" = 1 ] && return 1            # -y skips optional steps
	[ -r /dev/tty ] || return 1
	printf '%s [y/N] ' "$1"
	read -r a < /dev/tty || return 1
	case "$a" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# Offer to remove the managed Steam collections before deleting the state that
# tracks them (otherwise they're left orphaned in Steam).
if [ -n "$bin" ] && [ -f "$STATE/managed.json" ]; then
	if prompt "Remove the Steam collections DeckDex created first? (recommended)"; then
		"$bin" prune < /dev/tty || printf 'prune did not complete; continuing with file removal.\n'
	fi
fi

printf '\nThe following will be deleted:\n'
for p in "$bin" "$CACHE" "$CONFIG" "$STATE"; do
	[ -n "$p" ] && [ -e "$p" ] && printf '  %s\n' "$p"
done

if [ "$YES" != 1 ] && ! prompt "Delete these?"; then
	printf 'Aborted; nothing removed.\n'
	exit 0
fi

[ -n "$bin" ] && rm -f "$bin"
rm -rf "$CACHE" "$CONFIG"
# STATE may be outside CONFIG when XDG_STATE_HOME is set.
case "$STATE" in "$CONFIG"/*) ;; *) rm -rf "$STATE" ;; esac

printf 'DeckDex removed.\n'
