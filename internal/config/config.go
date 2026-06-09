// Package config loads DeckDex's TOML configuration and the built-in presets
// that synthesise a collection set without a hand-written file.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the full on-disk configuration.
type Config struct {
	Steam       SteamConfig      `toml:"steam"`
	ProtonDB    ProtonDBConfig   `toml:"protondb"`
	Collections []CollectionSpec `toml:"collection"`
}

// SteamConfig holds Steam identification + override knobs (all optional).
type SteamConfig struct {
	APIKey    string `toml:"api_key"`
	SteamID64 string `toml:"steam_id64"`
	AccountID string `toml:"account_id"`
	Path      string `toml:"path"`
}

// ProtonDBConfig holds tiering/native + rate-limit knobs.
type ProtonDBConfig struct {
	CacheTTLDays int `toml:"cache_ttl_days"` // baseline established-game TTL; default 30
	// DetectNative adds ONE extra ProtonDB/Algolia (steamdb2) call per game to
	// flag native-Linux titles. Off by default — the default sweep is a pure
	// ProtonDB tier pull (1 summaries call per game).
	DetectNative bool `toml:"detect_native"`
	// FilterNonGames drops DLC/tools/soundtracks. Costs one Steam appdetails
	// proxy call per game (the only Steam-backed request), so it is off by
	// default to keep the sweep ProtonDB-only and fast.
	FilterNonGames bool    `toml:"filter_non_games"`
	NativeCanTier  bool    `toml:"native_can_tier"` // native games also eligible for tier collections
	MaxRPS         float64 `toml:"max_rps"`         // default 50
	MaxConcurrency int     `toml:"max_concurrency"` // default 16
}

// CollectionSpec is one [[collection]] block: name + the categories feeding it.
type CollectionSpec struct {
	Name  string   `toml:"name"`
	Tiers []string `toml:"tiers"`
}

// Defaults returns a Config with sensible zero-config defaults applied.
func Defaults() Config {
	return Config{
		ProtonDB: ProtonDBConfig{
			CacheTTLDays:   30,
			DetectNative:   false, // opt-in: adds a call per game
			FilterNonGames: false, // opt-in: adds a Steam-backed call per game
			NativeCanTier:  false,
			MaxRPS:         50,
			MaxConcurrency: 16,
		},
	}
}

// Load reads config from path. A missing file is not an error: defaults are
// returned (callers may still inject a preset). found reports whether a file
// was actually read.
func Load(path string) (cfg Config, found bool, err error) {
	cfg = Defaults()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	// Decode over the defaults so unspecified fields keep them, then re-apply
	// any field that TOML left at a zero we treat as "unset".
	loaded := Defaults()
	if _, err := toml.Decode(string(data), &loaded); err != nil {
		return cfg, true, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if loaded.ProtonDB.CacheTTLDays == 0 {
		loaded.ProtonDB.CacheTTLDays = 30
	}
	if loaded.ProtonDB.MaxRPS == 0 {
		loaded.ProtonDB.MaxRPS = 50
	}
	if loaded.ProtonDB.MaxConcurrency == 0 {
		loaded.ProtonDB.MaxConcurrency = 16
	}
	return loaded, true, nil
}

// AllCategories is the full set of resolved category labels.
var AllCategories = []string{"native", "platinum", "gold", "silver", "bronze", "borked", "pending"}

// Preset returns the collection set for a named preset, or an error for an
// unknown name. Presets override the in-memory collection set for a run.
func Preset(name string) ([]CollectionSpec, error) {
	switch name {
	case "per-tier":
		out := make([]CollectionSpec, 0, len(AllCategories))
		for _, c := range AllCategories {
			out = append(out, CollectionSpec{Name: "ProtonDB · " + title(c), Tiers: []string{c}})
		}
		return out, nil
	case "playable":
		return []CollectionSpec{{Name: "ProtonDB · Playable", Tiers: []string{"native", "platinum", "gold"}}}, nil
	case "borked-only":
		return []CollectionSpec{{Name: "ProtonDB · Borked", Tiers: []string{"borked"}}}, nil
	case "native-only":
		return []CollectionSpec{{Name: "Native Linux", Tiers: []string{"native"}}}, nil
	default:
		return nil, fmt.Errorf("config: unknown preset %q (have: per-tier, playable, borked-only, native-only)", name)
	}
}

func title(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}
