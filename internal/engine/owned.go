// Package engine orchestrates the sync: resolving the owned library, deriving a
// ProtonDB tier + native category per game, and planning collection membership.
package engine

import (
	"context"
	"fmt"

	"github.com/nobodys-tools/DeckDex/internal/config"
	"github.com/nobodys-tools/DeckDex/internal/steam"
	"github.com/nobodys-tools/DeckDex/internal/steamapi"
)

// Game is an owned game plus an optional native-Linux hint carried from the
// source. (Steam's GetOwnedGames carries no platform flag, so this is normally
// nil and native is resolved per-app later.)
type Game struct {
	AppID      uint32
	Name       string
	NativeHint *bool // nil = unknown, resolve later
}

// OwnedResult is the resolved owned set and a human label for the source used.
type OwnedResult struct {
	Games  []Game
	Source string
}

// ResolveOwned picks an owned-games source by priority:
//  1. Steam Web API key + the locally-detected SteamID64 — returns the full
//     owned library (a key reads its own account even if the profile is private).
//  2. local Steam files — offline fallback; covers installed/played games only.
func ResolveOwned(ctx context.Context, cfg config.Config, root *steam.Root, acc *steam.Account) (OwnedResult, error) {
	if cfg.Steam.APIKey != "" {
		games, err := steamapi.GetOwnedGames(ctx, cfg.Steam.APIKey, acc.SteamID64)
		if err == nil && len(games) > 0 {
			out := make([]Game, 0, len(games))
			for _, g := range games {
				out = append(out, Game{AppID: g.AppID, Name: g.Name})
			}
			return OwnedResult{Games: out, Source: "Steam Web API"}, nil
		}
		if err != nil {
			fmt.Printf("warning: Steam Web API fetch failed (%v); falling back to local files\n", err)
		} else {
			fmt.Println("warning: Steam Web API returned no games (private game-details profile?); falling back to local files")
		}
	}

	local, err := root.LocalOwnedGames(acc)
	if err != nil {
		return OwnedResult{}, fmt.Errorf("no owned-games source succeeded: %w", err)
	}
	if len(local) == 0 {
		return OwnedResult{}, fmt.Errorf("no owned games found; set a Steam Web API key (--api-key / [steam].api_key) for the full library — local files only cover installed/played games")
	}
	out := make([]Game, 0, len(local))
	for _, g := range local {
		out = append(out, Game{AppID: g.AppID, Name: g.Name})
	}
	return OwnedResult{Games: out, Source: "local Steam files (installed/played only — may be incomplete; add an API key for the full library)"}, nil
}
