// Package collections reads and writes Steam's cloud-storage collections file
// (cloud-storage-namespace-1.json) — the authoritative local representation
// Steam syncs to the cloud.
//
// The file is a JSON array of [key, entry] two-element arrays. Entries DeckDex
// does not manage are passed through byte-for-byte; only user-collections.*
// entries we own are rewritten, with a bumped version so Steam accepts the edit.
package collections

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Payload is the decoded `value` of a user-collections entry.
type Payload struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Added   []uint32 `json:"added"`
	Removed []uint32 `json:"removed"`
}

// entryView is the subset of an entry object we read/write. Unknown fields on
// pass-through entries are preserved because we keep their raw bytes.
type entryView struct {
	Key       string `json:"key"`
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value,omitempty"`
	Version   string `json:"version"`
	IsDeleted bool   `json:"is_deleted,omitempty"`
}

// pair is one [key, entry] element of the array.
type pair struct {
	key string
	raw json.RawMessage // the entry object, verbatim
	dec entryView       // best-effort decode for matching/versioning
}

// Namespace is a parsed collections file.
type Namespace struct {
	pairs      []pair
	maxVersion int
}

// Parse reads the JSON array form. A nil/empty input yields an empty namespace.
func Parse(data []byte) (*Namespace, error) {
	ns := &Namespace{}
	if len(data) == 0 {
		return ns, nil
	}
	var rows [][2]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("collections: parse namespace file: %w", err)
	}
	for _, row := range rows {
		var key string
		_ = json.Unmarshal(row[0], &key)
		var dec entryView
		_ = json.Unmarshal(row[1], &dec)
		ns.pairs = append(ns.pairs, pair{key: key, raw: row[1], dec: dec})
		if v, err := strconv.Atoi(dec.Version); err == nil && v > ns.maxVersion {
			ns.maxVersion = v
		}
	}
	return ns, nil
}

// CollectionInfo summarises a live collection for `list`.
type CollectionInfo struct {
	ID      string
	Name    string
	Size    int
	Managed bool
}

// List returns every non-tombstone user collection with its membership size.
func (ns *Namespace) List(managedIDs map[string]bool) []CollectionInfo {
	var out []CollectionInfo
	for _, p := range ns.pairs {
		if p.dec.IsDeleted || p.dec.Value == "" || !isUserCollectionKey(p.key) {
			continue
		}
		var pl Payload
		if json.Unmarshal([]byte(p.dec.Value), &pl) != nil {
			continue
		}
		out = append(out, CollectionInfo{
			ID:      pl.ID,
			Name:    pl.Name,
			Size:    len(pl.Added),
			Managed: managedIDs[pl.ID],
		})
	}
	return out
}

// findIndex locates a user-collections entry by id (preferred) or name,
// skipping tombstones. Returns -1 when absent.
func (ns *Namespace) findIndex(id, name string) int {
	// Pass 1: match by id.
	for i, p := range ns.pairs {
		if p.dec.IsDeleted || p.dec.Value == "" || !isUserCollectionKey(p.key) {
			continue
		}
		var pl Payload
		if json.Unmarshal([]byte(p.dec.Value), &pl) != nil {
			continue
		}
		if id != "" && pl.ID == id {
			return i
		}
	}
	// Pass 2: match by name.
	for i, p := range ns.pairs {
		if p.dec.IsDeleted || p.dec.Value == "" || !isUserCollectionKey(p.key) {
			continue
		}
		var pl Payload
		if json.Unmarshal([]byte(p.dec.Value), &pl) != nil {
			continue
		}
		if name != "" && pl.Name == name {
			return i
		}
	}
	return -1
}

// Set creates or updates a static managed collection. id may be empty to mint a
// new one (returned). appids becomes the full `added` membership (sorted);
// `removed` is left empty. Each call bumps the namespace version.
func (ns *Namespace) Set(id, name string, appids []uint32, now time.Time) (string, error) {
	idx := ns.findIndex(id, name)
	if id == "" {
		if idx >= 0 {
			// Reuse the existing collection's id.
			var pl Payload
			_ = json.Unmarshal([]byte(ns.pairs[idx].dec.Value), &pl)
			id = pl.ID
		}
		if id == "" {
			id = newID()
		}
	}

	// Start from a non-nil slice so an empty membership marshals as [] (not null).
	sorted := append([]uint32{}, appids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	payload := Payload{ID: id, Name: name, Added: sorted, Removed: []uint32{}}
	valueBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	ns.maxVersion++
	ev := entryView{
		Key:       "user-collections." + id,
		Timestamp: now.Unix(),
		Value:     string(valueBytes),
		Version:   strconv.Itoa(ns.maxVersion),
	}
	rawEntry, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}

	np := pair{key: ev.Key, raw: rawEntry, dec: ev}
	if idx >= 0 {
		ns.pairs[idx] = np
	} else {
		ns.pairs = append(ns.pairs, np)
	}
	return id, nil
}

// Remove turns a managed collection into a tombstone (is_deleted, no value), so
// Steam drops it on next sync. No-op if absent.
func (ns *Namespace) Remove(id string, now time.Time) {
	idx := ns.findIndex(id, "")
	if idx < 0 {
		return
	}
	ns.maxVersion++
	ev := entryView{
		Key:       "user-collections." + id,
		Timestamp: now.Unix(),
		Version:   strconv.Itoa(ns.maxVersion),
		IsDeleted: true,
	}
	raw, _ := json.Marshal(ev)
	ns.pairs[idx] = pair{key: ev.Key, raw: raw, dec: ev}
}

// Bytes serialises the namespace back to the [key, entry] array form.
func (ns *Namespace) Bytes() ([]byte, error) {
	rows := make([]json.RawMessage, 0, len(ns.pairs))
	for _, p := range ns.pairs {
		keyJSON, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		row := append([]byte{'['}, keyJSON...)
		row = append(row, ',')
		row = append(row, p.raw...)
		row = append(row, ']')
		rows = append(rows, row)
	}
	return json.Marshal(rows)
}

func isUserCollectionKey(key string) bool {
	const prefix = "user-collections."
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}

// newID mints a short random collection id.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a fixed-length zero token.
		return "dd000000000000000"
	}
	return "dd" + hex.EncodeToString(b)
}
