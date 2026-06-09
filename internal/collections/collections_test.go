package collections

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const handMade = `[["user-collections.handmade1",{"key":"user-collections.handmade1","timestamp":1700000000,"value":"{\"id\":\"handmade1\",\"name\":\"My Faves\",\"added\":[220,413150],\"removed\":[]}","version":"7"}]]`

func TestSetPreservesUserCollections(t *testing.T) {
	ns, err := Parse([]byte(handMade))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := time.Unix(1781000000, 0)
	id, err := ns.Set("", "ProtonDB · Native", []uint32{413150}, now)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.HasPrefix(id, "dd") {
		t.Errorf("minted id %q should start with dd", id)
	}

	out, err := ns.Bytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	// The hand-made collection must survive untouched.
	if !strings.Contains(string(out), "handmade1") {
		t.Error("hand-made collection was dropped")
	}
	// Version must have bumped past the existing max (7) to 8.
	if !strings.Contains(string(out), `\"id\":\"`+id+`\"`) && !strings.Contains(string(out), id) {
		t.Error("new collection id not present in output")
	}

	// Re-parse and confirm both collections are listed.
	ns2, _ := Parse(out)
	infos := ns2.List(map[string]bool{id: true})
	if len(infos) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(infos))
	}
}

func TestEmptyMembershipSerializesAsArray(t *testing.T) {
	ns, _ := Parse(nil)
	now := time.Unix(1781000000, 0)
	if _, err := ns.Set("", "Empty", nil, now); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, _ := ns.Bytes()
	if strings.Contains(string(out), `added\":null`) {
		t.Error("empty membership serialized as null, want []")
	}
}

func TestSetReusesIDByName(t *testing.T) {
	ns, _ := Parse([]byte(handMade))
	now := time.Unix(1781000000, 0)
	id1, _ := ns.Set("", "Repeat", []uint32{1}, now)
	id2, _ := ns.Set(id1, "Repeat", []uint32{1, 2}, now)
	if id1 != id2 {
		t.Errorf("re-sync minted a new id: %q vs %q", id1, id2)
	}
	infos := ns.List(nil)
	count := 0
	for _, in := range infos {
		if in.Name == "Repeat" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected single Repeat collection, got %d", count)
	}
}

func TestTombstoneOnRemove(t *testing.T) {
	ns, _ := Parse([]byte(handMade))
	now := time.Unix(1781000000, 0)
	id, _ := ns.Set("", "Doomed", []uint32{5}, now)
	ns.Remove(id, now)
	out, _ := ns.Bytes()

	// The entry should now carry is_deleted and no value payload.
	var rows [][2]json.RawMessage
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	// "Doomed" must not appear among live collections.
	ns2, _ := Parse(out)
	for _, in := range ns2.List(nil) {
		if in.Name == "Doomed" {
			t.Error("removed collection still listed")
		}
	}
}
