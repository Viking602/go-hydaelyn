package evaluation

import (
	"encoding/json"
	"testing"
)

func TestReplayInvariantSchema(t *testing.T) {
	t.Parallel()

	want := ReplayInvariantLevelStateEquivalentWithRequiredSubsetV1

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal replay invariant: %v", err)
	}

	if string(data) != `"StateEquivalentWithRequiredSubsetV1"` {
		t.Fatalf("unexpected replay invariant JSON: %s", string(data))
	}

	var got ReplayInvariantLevel
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal replay invariant: %v", err)
	}

	if got != want {
		t.Fatalf("round-trip mismatch: want %q got %q", want, got)
	}
}
