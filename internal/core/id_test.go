package core

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/memory"
)

var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDMintsPrefixedUUIDv7(t *testing.T) {
	rt := NewMemoryRuntime()
	for _, prefix := range []string{"lease", "env", "span", "msg"} {
		id := rt.newID(prefix)
		suffix, found := strings.CutPrefix(id, prefix+"-")
		if !found {
			t.Fatalf("newID(%q) = %q, want the prefix retained", prefix, id)
		}
		if !uuidV7Pattern.MatchString(suffix) {
			t.Fatalf("newID(%q) = %q, want a version-7 variant-1 UUID after the prefix", prefix, id)
		}
	}
}

// The bug this replaced was a counter that restarted with each Runtime, so a
// second Runtime resuming against a store re-minted IDs the store already
// held. Uniqueness must not depend on Runtime-local state.
func TestNewIDIsUniqueAcrossRuntimes(t *testing.T) {
	provider := memory.NewProvider()
	first := NewRuntime(Config{StoreProvider: provider})
	second := NewRuntime(Config{StoreProvider: provider})

	seen := map[string]string{}
	for _, runtime := range []struct {
		name string
		rt   *Runtime
	}{{"first", first}, {"second", second}} {
		for range 100 {
			id := runtime.rt.newID("lease")
			if owner, clash := seen[id]; clash {
				t.Fatalf("runtime %s re-minted %q already minted by %s", runtime.name, id, owner)
			}
			seen[id] = runtime.name
		}
	}
	if len(seen) != 200 {
		t.Fatalf("minted %d distinct ids across two runtimes, want 200", len(seen))
	}
}

// A process restart gives the runtime a zero-valued generator. Two such
// generators stand in for two processes writing to one durable store: any
// scheme seeded from process-local state (a counter, however scoped) mints
// the same first id in both and collides. UUIDv7 does not.
func TestFreshGeneratorsDoNotCollide(t *testing.T) {
	var restarted, original idGenerator

	seen := map[string]struct{}{}
	for range 100 {
		for _, generator := range []*idGenerator{&original, &restarted} {
			id := generator.next()
			if _, clash := seen[id]; clash {
				t.Fatalf("a restarted generator re-minted %q; ids depend on process-local state", id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestNewIDIsUniqueAndSortableUnderConcurrency(t *testing.T) {
	rt := NewMemoryRuntime()
	const goroutines, perGoroutine = 8, 200

	var wg sync.WaitGroup
	batches := make([][]string, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]string, perGoroutine)
			for j := range ids {
				ids[j] = rt.newID("env")
			}
			batches[i] = ids
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine)
	for _, batch := range batches {
		for _, id := range batch {
			if _, clash := seen[id]; clash {
				t.Fatalf("duplicate id %q under concurrency", id)
			}
			seen[id] = struct{}{}
		}
		// UUIDv7 is time-ordered, and a single goroutine's ids are minted in
		// sequence, so its own batch must already be sorted.
		if !slices.IsSorted(batch) {
			t.Fatal("ids minted in sequence are not lexicographically sorted")
		}
	}
	if len(seen) != goroutines*perGoroutine {
		t.Fatalf("minted %d distinct ids, want %d", len(seen), goroutines*perGoroutine)
	}
}

// The end-to-end shape of the original defect: a second Runtime resuming
// against a store that already holds a lease must acquire its own, not
// collide with the one left behind.
func TestSecondRuntimeAcquiresLeaseWithoutIDCollision(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()

	first := NewRuntime(Config{StoreProvider: provider})
	run, task := mustStartWorker(ctx, t, first, "run-id-collision", "worker")
	abandoned := leaseTask(ctx, t, first, run.ID, task.ID, HolderAgent, "agent-a")

	second := NewRuntime(Config{StoreProvider: provider})
	other := mustCreateTask(ctx, t, second, CreateTaskCommand{RunID: run.ID, TaskID: "worker-2", Type: api.TaskTypeWorker, OwnerAgentID: "agent-b"})
	fresh := leaseTask(ctx, t, second, run.ID, other.ID, HolderAgent, "agent-b")

	if fresh.ID == abandoned.ID {
		t.Fatalf("resuming runtime re-minted lease id %q", fresh.ID)
	}
	if fresh.TaskID != other.ID {
		t.Fatalf("resuming runtime acquired lease for %q, want %q", fresh.TaskID, other.ID)
	}
}
