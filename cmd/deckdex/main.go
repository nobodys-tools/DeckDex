// Command deckdex indexes a Steam library into Steam Collections grouped by
// ProtonDB tier. It is a headless CLI: no TUI, no GUI.
//
// Tier data is sourced from ProtonDB's public endpoints. ProtonDB report data is
// licensed ODbL; DeckDex only reads it at runtime into a local cache and never
// redistributes the dataset. Steam is a trademark of Valve Corporation; this
// project is not affiliated with Valve or ProtonDB.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// globals collects the shared flags accepted by every subcommand.
type globals struct {
	configPath     string
	steamPath      string
	accountID      string
	apiKey         string
	preset         string
	native         bool
	noNative       bool
	filterNonGames bool
	maxRPS         float64
	dryRun         bool
	yes            bool
	noCache        bool
	cacheTTL       int
	killSteam      bool
	verbose        bool
}

func registerCommonFlags(fs *flag.FlagSet, g *globals) {
	fs.StringVar(&g.configPath, "config", "", "path to config.toml (default: OS config dir)")
	fs.StringVar(&g.steamPath, "steam-path", "", "override Steam root directory")
	fs.StringVar(&g.accountID, "account-id", "", "Steam userdata account id (folder name)")
	fs.StringVar(&g.apiKey, "api-key", "", "Steam Web API key for the full owned-games list")
	fs.StringVar(&g.preset, "preset", "", "collection preset: per-tier|playable|borked-only|native-only")
	fs.BoolVar(&g.native, "native", false, "detect native-Linux games (a few batched Algolia queries)")
	fs.BoolVar(&g.noNative, "no-native", false, "skip native detection even if a collection uses the 'native' category")
	fs.BoolVar(&g.filterNonGames, "filter-non-games", false, "drop DLC/tools/soundtracks (one extra Steam appdetails call per game)")
	fs.Float64Var(&g.maxRPS, "max-rps", 0, "override ProtonDB request rate in req/s (default 50)")
	fs.BoolVar(&g.dryRun, "dry-run", false, "print the plan and write nothing")
	fs.BoolVar(&g.yes, "yes", false, "skip the confirmation prompt for writes")
	fs.BoolVar(&g.noCache, "no-cache", false, "force refresh of all tier/native data")
	fs.IntVar(&g.cacheTTL, "cache-ttl", 0, "override established-game cache TTL in days")
	fs.BoolVar(&g.killSteam, "kill-steam", false, "close a running Steam client before writing")
	fs.BoolVar(&g.verbose, "verbose", false, "verbose output")
}

func usage() {
	fmt.Fprint(os.Stderr, `deckdex — sync Steam Collections by ProtonDB tier

Usage:
  deckdex <command> [flags]

Commands:
  detect    Print OS, Steam root, accounts, API-key status, paths.
  fetch     Resolve the owned-games list and report its source + count.
  tiers     Resolve ProtonDB tier + native flag for owned games; report counts.
  plan      Show collections and per-collection membership diff; write nothing.
  sync      Write/update managed collections (honours --dry-run / --yes).
  list      List current Steam collections (managed + user) with sizes.
  prune     Remove managed collections (restore library to pre-tool state).
  restore   Restore the most recent collections backup.
  version   Print version and data attribution.

Common flags (after the command):
  --config <path>      --steam-path <path>    --account-id <id>
  --api-key <key>      --preset <name>        --native / --no-native
  --filter-non-games   --max-rps <n>          --dry-run
  --yes                --no-cache             --cache-ttl <days>
  --kill-steam         --verbose

A free Steam Web API key (https://steamcommunity.com/dev/apikey) gives the full
owned-games list; without one, only installed/played games are seen.

By default the tier sweep is a pure ProtonDB pull: one tier request per game.
--native adds native-Linux detection via a few batched Algolia queries (cheap);
it auto-enables when a collection uses the 'native' category — pass --no-native
to skip it (that collection stays empty). --filter-non-games adds one Steam
appdetails call per game. On rate limits, lower --max-rps; results are cached.

Examples:
  deckdex detect
  deckdex fetch --api-key <your-key>
  deckdex sync --preset per-tier --api-key <your-key> --dry-run
  deckdex sync --preset per-tier --native --api-key <your-key> --yes
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "detect":
		err = cmdDetect(args)
	case "fetch":
		err = cmdFetch(args)
	case "tiers":
		err = cmdTiers(args)
	case "plan":
		err = cmdPlan(args)
	case "sync":
		err = cmdSync(args)
	case "list":
		err = cmdList(args)
	case "prune", "reset":
		err = cmdPrune(args)
	case "restore":
		err = cmdRestore(args)
	case "version", "--version", "-v":
		fmt.Printf("deckdex %s\nTier data: ProtonDB (https://www.protondb.com), report data licensed ODbL.\n", version)
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "deckdex: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "deckdex: %v\n", err)
		os.Exit(1)
	}
}
