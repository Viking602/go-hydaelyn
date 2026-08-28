package session

import (
	"strings"
	"testing"
)

func TestIDGenerator_UniqueVersionAndFollowerTime(t *testing.T) {
	gen := NewIDGenerator()
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := gen.Next()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = struct{}{}
		if versionNibble(id) != '7' {
			t.Fatalf("id %s version nibble = %q, want 7", id, versionNibble(id))
		}
	}

	leader := gen.Next()
	ms, ok := TimestampMs(leader)
	if !ok {
		t.Fatalf("TimestampMs(%s) reported an unparsable id", leader)
	}
	follower := gen.Next(ms)
	if timeHex(leader) != timeHex(follower) {
		t.Fatalf("follower time %s != leader time %s", timeHex(follower), timeHex(leader))
	}
	if versionNibble(follower) != '7' {
		t.Fatalf("follower version nibble = %q, want 7", versionNibble(follower))
	}
}

func versionNibble(id string) byte {
	if len(id) < 15 {
		return 0
	}
	return id[14]
}

func timeHex(id string) string {
	return strings.ReplaceAll(id, "-", "")[:12]
}

func TestTimestampMs(t *testing.T) {
	minted := NewIDGenerator().Next(1_700_000_000_000)
	wrongVersion := minted[:14] + "4" + minted[15:]
	wrongVariant := minted[:19] + "0" + minted[20:]
	tests := []struct {
		name string
		id   string
		want int64
		ok   bool
	}{
		{name: "minted id round-trips", id: minted, want: 1_700_000_000_000, ok: true},
		{name: "dashless form is rejected", id: strings.ReplaceAll(minted, "-", "")},
		{name: "short id is rejected", id: "abc"},
		{name: "empty id is rejected"},
		{name: "non-hex id is rejected", id: "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"},
		{name: "wrong UUID version is rejected", id: wrongVersion},
		{name: "wrong UUID variant is rejected", id: wrongVariant},
		{name: "trailing bytes are rejected", id: minted + "00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TimestampMs(tt.id)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("TimestampMs(%q) = %d, %v; want %d, %v", tt.id, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestIDGenerator_PinnedTimestampKeepsMonotonic(t *testing.T) {
	gen := NewIDGenerator()
	first := gen.Next()
	at, ok := TimestampMs(first)
	if !ok {
		t.Fatalf("TimestampMs(%s) reported an unparsable id", first)
	}

	// Pin far into the past, the way a follower id is pinned behind the entry
	// it belongs to, then go back to unpinned minting.
	gen.Next(at - 5_000)
	gen.Next(at - 5_000)

	// Ordering lives in the timestamp and sequence fields; the random suffix
	// would decide a tie by coin flip and hide the regression.
	previous := first
	for range 50 {
		id := gen.Next()
		if sortKey(id) <= sortKey(previous) {
			t.Fatalf("id %s did not advance past %s; the pinned call dragged the generator backwards", id, previous)
		}
		previous = id
	}
}

// sortKey returns the ms-and-sequence prefix that makes UUIDv7 values sortable,
// excluding the random bits.
func sortKey(id string) string {
	return strings.ReplaceAll(id, "-", "")[:16]
}

func TestIDGenerator_PinnedTimestampIsClampedNotHonoredBackwards(t *testing.T) {
	gen := NewIDGenerator()
	leader := gen.Next()
	at, ok := TimestampMs(leader)
	if !ok {
		t.Fatalf("TimestampMs(%s) reported an unparsable id", leader)
	}
	stale, ok := TimestampMs(gen.Next(at - 1_000))
	if !ok {
		t.Fatal("follower id is unparsable")
	}
	if stale < at {
		t.Fatalf("follower timestamp %d is older than the last minted %d", stale, at)
	}
}
