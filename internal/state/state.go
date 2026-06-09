// Package state tracks the collections DeckDex itself created, so re-syncs
// update the right entries (even if the user renamed one in the Steam UI) and
// prune/reset can remove only managed collections — never the user's own
// hand-made ones.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nobodys-tools/DeckDex/internal/paths"
)

// Managed records one collection owned by the tool.
type Managed struct {
	Name      string   `json:"name"`      // last-written display name
	ID        string   `json:"id"`        // stable collection id we minted
	LastAppIDs []uint32 `json:"last_appids"` // membership last written
}

// State is the persisted set of managed collections, keyed by our stable id.
type State struct {
	path    string
	Managed map[string]*Managed `json:"managed"` // id -> record
}

// Open loads (or initialises) the state file from the OS state dir.
func Open() (*State, error) {
	dir, err := paths.StateDir()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDir(dir); err != nil {
		return nil, err
	}
	s := &State{path: filepath.Join(dir, "managed.json"), Managed: map[string]*Managed{}}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var on struct {
		Managed map[string]*Managed `json:"managed"`
	}
	if json.Unmarshal(data, &on) == nil && on.Managed != nil {
		s.Managed = on.Managed
	}
	return s, nil
}

// ByName returns the managed record whose last-written name matches, if any.
func (s *State) ByName(name string) (*Managed, bool) {
	for _, m := range s.Managed {
		if m.Name == name {
			return m, true
		}
	}
	return nil, false
}

// Upsert records (or updates) a managed collection.
func (s *State) Upsert(m *Managed) { s.Managed[m.ID] = m }

// Remove deletes a managed record by id.
func (s *State) Remove(id string) { delete(s.Managed, id) }

// Save writes the state file atomically.
func (s *State) Save() error {
	data, err := json.MarshalIndent(struct {
		Managed map[string]*Managed `json:"managed"`
	}{s.Managed}, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(s.path, data, 0o644)
}

// Path returns the state file location.
func (s *State) Path() string { return s.path }
