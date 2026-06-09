// Package cache is the on-disk tier/native cache keyed by AppID. It is the
// baseline for subsequent runs and a per-AppID staleness TTL keeps newer/
// less-settled games fresh while letting established tiers sit for weeks.
//
// Get/Put/Save are safe for concurrent use: the tier resolver hits the cache
// from many worker goroutines at once.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nobodys-tools/DeckDex/internal/paths"
)

// Entry is one cached game record.
type Entry struct {
	AppID       uint32    `json:"appid"`
	Tier        string    `json:"tier"`
	Native      bool      `json:"native"`
	Type        string    `json:"type,omitempty"` // "game", "dlc", ...
	Total       int       `json:"total"`          // ProtonDB report count
	ReleaseDate string    `json:"release_date,omitempty"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// Cache is the in-memory view persisted as a single JSON file.
type Cache struct {
	path    string
	mu      sync.RWMutex
	Entries map[uint32]Entry `json:"entries"`
}

// Open loads the cache from the OS cache dir, creating an empty one if absent.
func Open() (*Cache, error) {
	dir, err := paths.CacheDir()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDir(dir); err != nil {
		return nil, err
	}
	c := &Cache{path: filepath.Join(dir, "tiers.json"), Entries: map[uint32]Entry{}}
	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	// Tolerate a corrupt cache by starting fresh rather than failing the run.
	var on struct {
		Entries map[uint32]Entry `json:"entries"`
	}
	if json.Unmarshal(data, &on) == nil && on.Entries != nil {
		c.Entries = on.Entries
	}
	return c, nil
}

// Get returns the cached entry for appID. Safe for concurrent use.
func (c *Cache) Get(appID uint32) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.Entries[appID]
	return e, ok
}

// Put stores/overwrites an entry, stamping FetchedAt to now. Safe for
// concurrent use.
func (c *Cache) Put(e Entry, now time.Time) {
	e.FetchedAt = now
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Entries[e.AppID] = e
}

// Save writes the cache atomically.
func (c *Cache) Save() error {
	c.mu.RLock()
	data, err := json.MarshalIndent(struct {
		Entries map[uint32]Entry `json:"entries"`
	}{c.Entries}, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(c.path, data, 0o644)
}

// Path returns the cache file location (for `detect`/diagnostics).
func (c *Cache) Path() string { return c.path }

// IsStale reports whether the entry must be re-fetched, given the established
// baseline TTL (in days) and the current time. The staleness rules:
//   - recently released (release within 30 days): 24h TTL,
//   - pending / low report total (< 5): ~3 day TTL,
//   - otherwise: the configured baseline TTL.
func IsStale(e Entry, baselineTTLDays int, now time.Time) bool {
	age := now.Sub(e.FetchedAt)

	if recentlyReleased(e.ReleaseDate, now) {
		return age > 24*time.Hour
	}
	if e.Tier == "pending" || e.Total < 5 {
		return age > 3*24*time.Hour
	}
	ttl := time.Duration(baselineTTLDays) * 24 * time.Hour
	return age > ttl
}

// recentlyReleased reports whether a Steam release-date string is within the
// last 30 days. Steam's date format varies ("2 May, 2024", "May 2, 2024"); we
// try a few layouts and fail closed (false) when unparseable.
func recentlyReleased(date string, now time.Time) bool {
	if date == "" {
		return false
	}
	layouts := []string{"2 Jan, 2006", "Jan 2, 2006", "Jan 2006", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.Parse(l, date); err == nil {
			return now.Sub(t) < 30*24*time.Hour && !t.After(now)
		}
	}
	return false
}

// AppIDKey renders an appID as the string form used in some external contexts.
func AppIDKey(id uint32) string { return strconv.FormatUint(uint64(id), 10) }
