package engine

import (
	"sort"

	"github.com/nobodys-tools/DeckDex/internal/config"
)

// PlannedCollection is one collection's computed membership.
type PlannedCollection struct {
	Name   string
	Tiers  []string
	AppIDs []uint32
}

// Plan computes membership for each configured collection: the union of owned
// games whose resolved category is in the collection's tiers list. The reserved
// token "all" matches every category. Non-game filtering is applied when
// enabled. names maps AppID->display name for diff output.
func Plan(specs []config.CollectionSpec, resolved []Resolved, filterNonGames bool) ([]PlannedCollection, map[uint32]string) {
	names := make(map[uint32]string, len(resolved))
	for _, r := range resolved {
		names[r.AppID] = r.Name
	}

	out := make([]PlannedCollection, 0, len(specs))
	for _, spec := range specs {
		want := map[string]bool{}
		all := false
		for _, t := range spec.Tiers {
			if t == "all" {
				all = true
			}
			want[t] = true
		}

		var ids []uint32
		for _, r := range resolved {
			if filterNonGames && !r.IsGame() {
				continue
			}
			if all || want[r.Category] {
				ids = append(ids, r.AppID)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out = append(out, PlannedCollection{Name: spec.Name, Tiers: spec.Tiers, AppIDs: ids})
	}
	return out, names
}

// DiffAgainst compares planned membership to a previous AppID set, returning the
// added and removed AppIDs (sorted).
func DiffAgainst(planned []uint32, previous []uint32) (added, removed []uint32) {
	prev := map[uint32]bool{}
	for _, id := range previous {
		prev[id] = true
	}
	cur := map[uint32]bool{}
	for _, id := range planned {
		cur[id] = true
		if !prev[id] {
			added = append(added, id)
		}
	}
	for _, id := range previous {
		if !cur[id] {
			removed = append(removed, id)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	return added, removed
}
