# DeckDex

> Steam **Deck** + **Dex** — a Pokédex-style index of your Steam library by ProtonDB tier.

A headless CLI (`deckdex`) that organises your Steam library into **Steam Collections grouped by
ProtonDB tier** (platinum / gold / silver / bronze / borked / native / pending), so you
can see at a glance what runs well on Linux / Steam Deck — and keep those collections in
sync as ProtonDB ratings change.

It never touches collections you made by hand, and it backs up Steam's collection file
before every write.

## What it does

1. Discovers your local Steam install and active user (Windows / macOS / Linux, incl.
   Flatpak & Snap).
2. Reads your owned games via a free **Steam Web API key** (your SteamID is auto-detected
   locally; the key reads your library even if your profile is private). Without a key it
   falls back to local files, which only see installed/played games.
3. Looks up each game's **ProtonDB tier** (one request per game — a pure ProtonDB pull),
   cached locally with a staleness policy so re-runs barely hit the network. **Native-Linux**
   detection and **non-game filtering** are opt-in (`--native` / `--filter-non-games`), each
   adding one extra request per game.
4. Writes/updates **static Steam Collections** grouped by tier, per your config (e.g. one
   collection per tier, a single "Playable" = platinum+gold+native, "Borked only", etc.).

## Install

Prebuilt binaries are published on every release (linux/windows/macOS × amd64/arm64).

**Quick start (recommended)** — downloads the right binary, confirms it found your Steam
install, optionally takes a Steam key, then syncs:

```sh
curl -fsSL https://raw.githubusercontent.com/nobodys-tools/DeckDex/main/install.sh | sh
```

**Manual** — grab the binary for your platform from the
[latest release](https://github.com/nobodys-tools/DeckDex/releases/latest), make it
executable, and put it on your `PATH` as `deckdex`:

```sh
chmod +x deckdex-linux-amd64 && mv deckdex-linux-amd64 ~/.local/bin/deckdex
```

## Usage

### 1. Get a Steam Web API key (recommended)

Grab a free key at **<https://steamcommunity.com/dev/apikey>** (any domain works, e.g.
`localhost`). It lets DeckDex read your **full** owned library — even if your profile is
private. Without a key, DeckDex falls back to local files and only sees installed/played
games.

You can pass the key with `--api-key`, set it in the config file, or export
`DECKDEX_STEAM_API_KEY` (the env var keeps it out of your shell history and process list).

### 2. Run the recommended command

```sh
deckdex sync --preset per-tier --api-key <your-key>
```

This creates one Steam collection per ProtonDB tier (and native). It previews the changes
and asks before writing, backs up your collections file first, and never touches
collections you made by hand. Drop `--api-key` to run against local games only.

> **Close Steam before writing.** Steam caches collections client-side and may overwrite
> changes made while it's running. When run interactively, `sync`/`prune` **offer to close
> Steam** for you; otherwise quit Steam first, or pass `--kill-steam`.

### Unattended / cron

For a scheduled sync, run it non-interactively — provide the key via the
`DECKDEX_STEAM_API_KEY` env var (keeps it out of `ps`/history), and pass `--kill-steam --yes`
so it closes Steam and writes without prompting:

```sh
DECKDEX_STEAM_API_KEY=xxxx deckdex sync --preset per-tier --kill-steam --yes
```

### Other commands

```sh
deckdex detect                              # show OS, Steam root, account, paths
deckdex fetch --api-key <your-key>          # list your full owned library
deckdex tiers --api-key <your-key>          # resolve tiers; print counts per category
deckdex sync --preset per-tier --dry-run    # preview collections + membership diff (no write)
deckdex list                                # list current collections (managed + your own)
deckdex prune                               # remove only the collections DeckDex created
deckdex restore                             # restore the most recent backup
```

Presets (`--preset`): `per-tier`, `playable`, `borked-only`, `native-only`. Native
detection auto-enables for presets that use it (pass `--no-native` to skip). For full
control, define `[[collection]]` blocks in your config instead.

## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/nobodys-tools/DeckDex/main/uninstall.sh | sh
```

It offers to remove the Steam collections DeckDex created (via `deckdex prune`) first, then
deletes the binary, cache, config, state and backups. Add `-y` to skip prompts.

Manual cleanup instead (leaves your Steam collections in place — run `deckdex prune` first
if you want those gone too):

```sh
deckdex prune                              # optional: remove the collections it created
rm -rf ~/.local/bin/deckdex ~/.cache/deckdex ~/.config/deckdex   # binary, cache, config, state, backups
```

## Build from source

For contributors — the build runs entirely in Docker, **no host Go toolchain needed**. A
pinned `golang:` image cross-compiles static binaries for linux/windows/darwin ×
amd64/arm64 into `./dist`:

```sh
make build        # → ./dist/deckdex-<os>-<arch>[.exe]
make test         # unit tests        make vet     # go vet
make deps         # regenerate go.sum  make clean   # remove ./dist
```

Releases (tag + binaries) are produced automatically by `.github/workflows/release.yml`.

## Configuration

Config lives at your platform config dir (`~/.config/deckdex/config.toml`,
`%AppData%\deckdex\config.toml`, `~/Library/Application Support/deckdex/config.toml`) or
via `--config <path>`. It holds your (free) Steam Web API key and your tier→collection
groupings. See [`config.example.toml`](config.example.toml). The key can also be passed
ad-hoc with `--api-key`.

## How it works (data sources)

- **Owned games:** Steam Web API `GetOwnedGames` (with your locally-detected SteamID64;
  the full library) → local Steam VDF files (offline fallback, installed/played only).
  ProtonDB has no key-less library endpoint — its own site calls this same Steam API — so a
  free Steam Web API key is the reliable route.
- **Tier:** ProtonDB's per-app published-tier summary endpoint, cached. A short TTL for
  recently-released / `pending` / low-report games; a long TTL for settled tiers.
- **Native (opt-in, `--native`):** the `oslist` from ProtonDB's steamdb2/Algolia metadata
  index (`"Linux"` present ⇒ native; also yields Steam Deck status). One extra ProtonDB call
  per game; off by default.
- **Collections:** read/written in Steam's local cloud-storage JSON
  (`cloud-storage-namespace-1.json`), with a bumped version so Steam accepts the edit, and
  a timestamped backup before every write.

## License

**GNU GPL v3.0** — see [`LICENSE`](LICENSE).

## Attribution

- **ProtonDB** — tier data is sourced from ProtonDB's public endpoints. ProtonDB report
  data is licensed [ODbL](https://opendatacommons.org/licenses/odbl/); this tool only reads
  it at runtime into a local cache and does not redistribute the dataset.
- **Steam** is a trademark of Valve Corporation. This project is not affiliated with or
  endorsed by Valve or ProtonDB.

## Disclaimer

**This project was written by an AI coding agent.** Review the code before trusting it with
your Steam data.

This tool reads undocumented ProtonDB/Steam endpoints and writes to Steam's local
collection store. Use it considerately (it rate-limits itself) and at your own risk; always
keep the automatic backups it creates.
