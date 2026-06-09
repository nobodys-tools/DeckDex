package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nobodys-tools/DeckDex/internal/cache"
	"github.com/nobodys-tools/DeckDex/internal/config"
	"github.com/nobodys-tools/DeckDex/internal/protondb"
)

// Resolved is the final per-game classification.
type Resolved struct {
	AppID    uint32
	Name     string
	Tier     string
	Native   bool
	Type     string
	Category string // the resolved category label fed to collections
}

// Resolver derives categories for owned games, populating the cache.
type Resolver struct {
	Client *protondb.Client
	Cache  *cache.Cache
	Cfg    config.Config
	Now    time.Time
	// NoCache forces a refetch of every game regardless of staleness.
	NoCache bool
	// ForceRefresh is a set of AppIDs to refetch even if fresh.
	ForceRefresh map[uint32]bool
	// Progress, if set, is called periodically (and once at the end) with the
	// number of games resolved so far and the total. It is invoked from a single
	// goroutine, so the callback need not be concurrency-safe itself.
	Progress func(done, total int)

	// nativeMap is populated once (batched) before the worker pool when native
	// detection is enabled; resolveOne reads it instead of making a call per game.
	nativeMap map[uint32]bool
}

// needsFetch reports the cached entry and whether it must be (re)fetched.
func (r *Resolver) needsFetch(g Game) (cache.Entry, bool) {
	e, cached := r.Cache.Get(g.AppID)
	stale := r.NoCache || r.ForceRefresh[g.AppID] || !cached || cache.IsStale(e, r.Cfg.ProtonDB.CacheTTLDays, r.Now)
	return e, stale
}

// ResolveAll classifies every game, fetching tier/native/type as needed (cache
// misses or stale entries only) with bounded concurrency. The cache is updated
// in place; callers persist it afterwards.
func (r *Resolver) ResolveAll(ctx context.Context, games []Game) ([]Resolved, error) {
	conc := r.Cfg.ProtonDB.MaxConcurrency
	if conc <= 0 {
		conc = 16
	}
	// Native detection: resolve all stale games in a few batched Algolia queries
	// up front, rather than one throttled call per game.
	if r.Cfg.ProtonDB.DetectNative {
		var need []uint32
		for _, g := range games {
			if g.NativeHint != nil {
				continue
			}
			if _, stale := r.needsFetch(g); stale {
				need = append(need, g.AppID)
			}
		}
		if len(need) > 0 {
			if m, err := r.Client.NativeBatch(ctx, need); err == nil {
				r.nativeMap = m
			} else {
				fmt.Printf("warning: native detection failed (%v); games will be tiered instead\n", err)
			}
		}
	}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex

	out := make([]Resolved, len(games))
	var failed int
	var done int64
	total := len(games)

	// Progress ticker: report completions periodically without spamming a line
	// per game. Stopped once all workers finish.
	stop := make(chan struct{})
	if r.Progress != nil {
		go func() {
			t := time.NewTicker(200 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					r.Progress(int(atomic.LoadInt64(&done)), total)
				}
			}
		}()
	}

	for i, g := range games {
		wg.Add(1)
		go func(i int, g Game) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := r.resolveOne(ctx, g)
			atomic.AddInt64(&done, 1)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++ // per-app failure — non-fatal, game is left pending
			}
			out[i] = res
		}(i, g)
	}
	wg.Wait()
	close(stop)
	if r.Progress != nil {
		r.Progress(int(atomic.LoadInt64(&done)), total) // final 100% tick
	}
	// A single app's lookup failing (e.g. a transient 403/timeout) must not kill
	// the whole run — those games are left pending. Only error if ALL failed,
	// which signals ProtonDB is unreachable or blocking us.
	if failed > 0 && failed == total {
		return out, fmt.Errorf("all %d ProtonDB tier lookups failed (ProtonDB unreachable or blocking requests?)", total)
	}
	if failed > 0 {
		fmt.Printf("warning: %d of %d games could not be looked up and were left pending\n", failed, total)
	}
	return out, nil
}

// resolveOne classifies a single game, consulting/refreshing the cache.
func (r *Resolver) resolveOne(ctx context.Context, g Game) (Resolved, error) {
	entry, stale := r.needsFetch(g)
	_, cached := r.Cache.Get(g.AppID)

	tier := entry.Tier
	native := entry.Native
	gtype := entry.Type
	total := entry.Total
	release := entry.ReleaseDate

	if stale {
		// Core pull: the authoritative tier from the live summary endpoint.
		// This is the ONLY request made by default — a pure ProtonDB tier pull.
		s, err := r.Client.Summary(ctx, g.AppID)
		if err != nil {
			// On failure keep any cached value rather than dropping the game.
			if !cached {
				return Resolved{AppID: g.AppID, Name: g.Name, Tier: "pending", Category: "pending"}, err
			}
		} else {
			tier = string(s.Tier)
			total = s.Total
		}

		// Native detection (opt-in): from the owned-source hint, else the batched
		// Algolia map computed before the worker pool. No per-game call here.
		if r.Cfg.ProtonDB.DetectNative {
			switch {
			case g.NativeHint != nil:
				native = *g.NativeHint
			default:
				native = r.nativeMap[g.AppID] // false if absent from the index
			}
		}

		// Optional non-game filtering needs the Steam-backed appdetails proxy
		// (type + release date). This is the only request that touches Steam, so
		// it is opt-in.
		if r.Cfg.ProtonDB.FilterNonGames && gtype == "" {
			if d, ok, err := r.Client.AppDetails(ctx, g.AppID); err == nil && ok {
				gtype = d.Type
				if release == "" {
					release = d.ReleaseDate
				}
			}
		}

		r.Cache.Put(cache.Entry{
			AppID:       g.AppID,
			Tier:        tier,
			Native:      native,
			Type:        gtype,
			Total:       total,
			ReleaseDate: release,
		}, r.Now)
	}

	if tier == "" {
		tier = "pending"
	}
	cat := resolveCategory(native, tier, r.Cfg.ProtonDB.NativeCanTier)
	return Resolved{
		AppID:    g.AppID,
		Name:     g.Name,
		Tier:     tier,
		Native:   native,
		Type:     gtype,
		Category: cat,
	}, nil
}

// resolveCategory applies the precedence: native wins unless config allows
// native games to also be tiered; otherwise the ProtonDB tier; else pending.
func resolveCategory(native bool, tier string, nativeCanTier bool) string {
	if native && !nativeCanTier {
		return "native"
	}
	if tier == "" {
		return "pending"
	}
	return tier
}

// IsGame reports whether a resolved type counts as a game (for non-game
// filtering). An empty/unknown type is treated as a game (fail open).
func (res Resolved) IsGame() bool {
	return res.Type == "" || res.Type == "game"
}

// CategoryCounts tallies resolved games by category for reporting.
func CategoryCounts(items []Resolved) []string {
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Category]++
	}
	cats := make([]string, 0, len(counts))
	for c := range counts {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	out := make([]string, 0, len(cats))
	for _, c := range cats {
		out = append(out, fmt.Sprintf("%-9s %d", c, counts[c]))
	}
	return out
}
