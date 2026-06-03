package hashline

import (
	"strconv"
	"sync"
	"testing"
)

func TestMemorySnapshotStore_RecordAndHead(t *testing.T) {
	s := NewMemorySnapshotStore()

	tag1 := s.Record("f.go", "a\nb\n")
	if tag1 != ComputeFileHash("a\nb\n") {
		t.Errorf("Record tag = %q, want computed tag", tag1)
	}
	head, ok := s.Head("f.go")
	if !ok {
		t.Fatal("Head should report the recorded version")
	}
	if head.Text != "a\nb\n" || head.Hash != tag1 {
		t.Errorf("Head = %#v, want text a\\nb\\n / tag %s", head, tag1)
	}

	// A second distinct version becomes the new head; the first is still
	// retrievable by its hash.
	tag2 := s.Record("f.go", "a\nB\n")
	if tag2 == tag1 {
		t.Fatal("distinct content must yield a distinct tag")
	}
	head, _ = s.Head("f.go")
	if head.Hash != tag2 {
		t.Errorf("Head hash = %s, want latest %s", head.Hash, tag2)
	}
	if _, ok := s.ByHash("f.go", tag1); !ok {
		t.Error("older version must remain retrievable by hash")
	}
}

func TestMemorySnapshotStore_RecordNormalizes(t *testing.T) {
	s := NewMemorySnapshotStore()
	bom := "\uFEFF"
	a := s.Record("f.go", bom+"a\r\nb\r\n")
	b := s.Record("f.go", "a\nb\n")
	if a != b {
		t.Errorf("Record must normalize before hashing: %q vs %q", a, b)
	}
	// Both record the same normalized content, so only one version exists and
	// its stored text is LF/BOM-free.
	head, _ := s.Head("f.go")
	if head.Text != "a\nb\n" {
		t.Errorf("stored text = %q, want normalized", head.Text)
	}
}

func TestMemorySnapshotStore_SameContentPromotesToHead(t *testing.T) {
	s := NewMemorySnapshotStore()
	v1 := s.Record("f.go", "1\n")
	v2 := s.Record("f.go", "2\n")
	// Re-record v1's content: it must return the same tag and become head.
	again := s.Record("f.go", "1\n")
	if again != v1 {
		t.Errorf("re-recording same content tag = %q, want %q", again, v1)
	}
	head, _ := s.Head("f.go")
	if head.Hash != v1 {
		t.Errorf("Head hash = %s, want promoted %s", head.Hash, v1)
	}
	// v2 is still present (not duplicated, not evicted at this size).
	if _, ok := s.ByHash("f.go", v2); !ok {
		t.Error("v2 should still be retained")
	}
	// No duplicate versions for the same content.
	s.mu.Lock()
	count := len(s.paths["f.go"].versions)
	s.mu.Unlock()
	if count != 2 {
		t.Errorf("versions = %d, want 2 (no duplicate for re-recorded content)", count)
	}
}

func TestMemorySnapshotStore_ByHashUnknown(t *testing.T) {
	s := NewMemorySnapshotStore()
	s.Record("f.go", "x\n")
	if _, ok := s.ByHash("f.go", "FFFF"); ok {
		t.Error("ByHash for an unknown tag must report no match")
	}
	if _, ok := s.ByHash("other.go", "0000"); ok {
		t.Error("ByHash for an unknown path must report no match")
	}
}

func TestMemorySnapshotStore_PerPathVersionCap(t *testing.T) {
	s := NewMemorySnapshotStore()
	tags := make([]string, 0, maxVersionsPerPath+3)
	for i := 0; i < maxVersionsPerPath+3; i++ {
		tags = append(tags, s.Record("f.go", "v"+strconv.Itoa(i)+"\n"))
	}
	// Only the last maxVersionsPerPath distinct versions are retained.
	s.mu.Lock()
	count := len(s.paths["f.go"].versions)
	s.mu.Unlock()
	if count != maxVersionsPerPath {
		t.Fatalf("versions = %d, want cap %d", count, maxVersionsPerPath)
	}
	// The three oldest were evicted; the most recent maxVersionsPerPath remain.
	for i, tag := range tags {
		_, ok := s.ByHash("f.go", tag)
		shouldExist := i >= 3
		if ok != shouldExist {
			t.Errorf("version %d (tag %s): retained=%v, want %v", i, tag, ok, shouldExist)
		}
	}
}

func TestMemorySnapshotStore_PathLRUEviction(t *testing.T) {
	s := NewMemorySnapshotStore()
	// Fill exactly maxPaths distinct paths.
	for i := 0; i < maxPaths; i++ {
		s.Record("p"+strconv.Itoa(i)+".go", "x\n")
	}
	// Touch p0 so it is most-recently-used, making p1 the LRU.
	s.Record("p0.go", "x2\n")

	// Admitting a new path must evict the LRU path (p1), not p0.
	s.Record("new.go", "y\n")

	if _, ok := s.Head("p1.go"); ok {
		t.Error("LRU path p1.go should have been evicted")
	}
	if _, ok := s.Head("p0.go"); !ok {
		t.Error("recently-touched p0.go must survive eviction")
	}
	if _, ok := s.Head("new.go"); !ok {
		t.Error("newly recorded path must be present")
	}
	// Total tracked paths never exceeds the cap.
	s.mu.Lock()
	total := len(s.paths)
	s.mu.Unlock()
	if total > maxPaths {
		t.Errorf("paths = %d, exceeds cap %d", total, maxPaths)
	}
}

func TestMemorySnapshotStore_SameContentDifferentPathIsolated(t *testing.T) {
	// Two paths with byte-identical content share a tag but must be tracked
	// independently: a ByHash on one path must never return the other's
	// snapshot, and invalidating one must not touch the other.
	s := NewMemorySnapshotStore()
	const content = "shared\ncontent\n"
	tagA := s.Record("a.go", content)
	tagB := s.Record("b.go", content)
	if tagA != tagB {
		t.Fatalf("identical content must hash to the same tag: %s vs %s", tagA, tagB)
	}

	gotA, okA := s.ByHash("a.go", tagA)
	gotB, okB := s.ByHash("b.go", tagB)
	if !okA || !okB {
		t.Fatalf("both paths should resolve their snapshot: a=%v b=%v", okA, okB)
	}
	if gotA.Path != "a.go" || gotB.Path != "b.go" {
		t.Errorf("snapshots not isolated by path: a.Path=%q b.Path=%q", gotA.Path, gotB.Path)
	}

	// A tag recorded only under a.go must not be visible under a third path.
	if _, ok := s.ByHash("c.go", tagA); ok {
		t.Error("a tag recorded under a.go must not resolve under an unrelated path")
	}

	// Invalidating a.go leaves b.go (same content) intact.
	s.Invalidate("a.go")
	if _, ok := s.ByHash("a.go", tagA); ok {
		t.Error("a.go should be gone after Invalidate")
	}
	if _, ok := s.ByHash("b.go", tagB); !ok {
		t.Error("b.go must survive a.go's invalidation despite identical content")
	}
}

func TestMemorySnapshotStore_PathEvictionBoundary(t *testing.T) {
	// Recording exactly maxPaths distinct paths must retain all of them; the
	// (maxPaths+1)-th admission evicts exactly one (the LRU), never more.
	s := NewMemorySnapshotStore()
	for i := 0; i < maxPaths; i++ {
		s.Record("p"+strconv.Itoa(i)+".go", "x\n")
	}
	s.mu.Lock()
	atCap := len(s.paths)
	s.mu.Unlock()
	if atCap != maxPaths {
		t.Fatalf("at capacity: paths = %d, want exactly %d", atCap, maxPaths)
	}
	// p0 was the first recorded, hence the LRU; admitting one more evicts it.
	s.Record("extra.go", "y\n")
	s.mu.Lock()
	afterAdmit := len(s.paths)
	s.mu.Unlock()
	if afterAdmit != maxPaths {
		t.Errorf("after admitting one more: paths = %d, want %d (exactly one evicted)", afterAdmit, maxPaths)
	}
	if _, ok := s.Head("p0.go"); ok {
		t.Error("the LRU path p0.go should have been evicted")
	}
	if _, ok := s.Head("extra.go"); !ok {
		t.Error("the newly admitted path must be present")
	}
}

func TestMemorySnapshotStore_PerPathCapExactBoundary(t *testing.T) {
	// At exactly maxVersionsPerPath distinct versions, all are retained; the
	// next distinct version evicts exactly the oldest.
	s := NewMemorySnapshotStore()
	tags := make([]string, maxVersionsPerPath)
	for i := 0; i < maxVersionsPerPath; i++ {
		tags[i] = s.Record("f.go", "v"+strconv.Itoa(i)+"\n")
	}
	for i, tag := range tags {
		if _, ok := s.ByHash("f.go", tag); !ok {
			t.Errorf("version %d must be retained at exactly the cap", i)
		}
	}
	// One more distinct version: the oldest (index 0) is evicted, the rest stay.
	extra := s.Record("f.go", "extra\n")
	if _, ok := s.ByHash("f.go", tags[0]); ok {
		t.Error("oldest version must be evicted past the cap")
	}
	for i := 1; i < maxVersionsPerPath; i++ {
		if _, ok := s.ByHash("f.go", tags[i]); !ok {
			t.Errorf("version %d must survive a single over-cap admission", i)
		}
	}
	if _, ok := s.ByHash("f.go", extra); !ok {
		t.Error("the newest version must be retained")
	}
}

func TestMemorySnapshotStore_InvalidateAndClear(t *testing.T) {
	s := NewMemorySnapshotStore()
	s.Record("a.go", "1\n")
	s.Record("b.go", "2\n")

	s.Invalidate("a.go")
	if _, ok := s.Head("a.go"); ok {
		t.Error("Invalidate should forget a.go")
	}
	if _, ok := s.Head("b.go"); !ok {
		t.Error("Invalidate must not affect other paths")
	}

	s.Clear()
	if _, ok := s.Head("b.go"); ok {
		t.Error("Clear should forget everything")
	}
}

func TestMemorySnapshotStore_ZeroValueUsable(t *testing.T) {
	var s MemorySnapshotStore
	tag := s.Record("f.go", "z\n")
	if got, ok := s.ByHash("f.go", tag); !ok || got.Text != "z\n" {
		t.Errorf("zero-value store should be usable: ok=%v text=%q", ok, got.Text)
	}
}

func TestMemorySnapshotStore_SatisfiesInterface(t *testing.T) {
	var _ SnapshotStore = NewMemorySnapshotStore()
}

// findTagCollision brute-forces two distinct short strings whose 16-bit
// hashline tags are equal. The tag space is only 2^16, so a colliding pair
// turns up well before the pigeonhole bound.
func findTagCollision(t *testing.T) (a, b string) {
	t.Helper()
	seen := make(map[string]string)
	for i := 0; i <= 1<<17; i++ {
		s := "// variant " + strconv.Itoa(i) + "\npackage p\n"
		tag := ComputeFileHash(Normalize(s).Text)
		if prev, ok := seen[tag]; ok {
			return prev, s
		}
		seen[tag] = s
	}
	t.Fatal("no 16-bit tag collision found within the search bound")
	return "", ""
}

func TestMemorySnapshotStore_RetainsCollidingContents(t *testing.T) {
	a, b := findTagCollision(t)
	if Normalize(a).Text == Normalize(b).Text {
		t.Fatal("collision helper returned identical content")
	}

	s := NewMemorySnapshotStore()
	tagA := s.Record("f.go", a)
	tagB := s.Record("f.go", b)
	if tagA != tagB {
		t.Fatalf("helper did not produce a tag collision: %q vs %q", tagA, tagB)
	}

	// Both distinct contents are retained side by side despite sharing the tag —
	// the second must not collapse onto (and erase) the first.
	if !s.ContainsText("f.go", a) {
		t.Error("first colliding version was lost")
	}
	if !s.ContainsText("f.go", b) {
		t.Error("second colliding version was lost")
	}

	// ByHash resolves the tag to the most recently recorded colliding version.
	got, ok := s.ByHash("f.go", tagB)
	if !ok {
		t.Fatalf("ByHash(%q) missing", tagB)
	}
	if got.Text != Normalize(b).Text {
		t.Errorf("ByHash text = %q, want latest %q", got.Text, Normalize(b).Text)
	}

	// Content that was never recorded is reported absent (the guard's whole
	// point: a colliding-but-unseen live file is not mistaken for a known one).
	if s.ContainsText("f.go", "// never recorded\npackage q\n") {
		t.Error("ContainsText reported unrecorded content as present")
	}
}

func TestMemorySnapshotStore_ExactContentDeduplicates(t *testing.T) {
	// Recording identical content twice must NOT create a second version — only
	// distinct content (including tag collisions) is retained separately.
	s := NewMemorySnapshotStore()
	s.Record("f.go", "a\nb\n")
	s.Record("f.go", "a\nb\n")
	h := s.paths["f.go"]
	if h == nil {
		t.Fatal("path history missing")
	}
	if len(h.versions) != 1 {
		t.Errorf("versions = %d, want 1 (identical content must dedup)", len(h.versions))
	}
}

func TestMemorySnapshotStore_ConcurrentAccess(t *testing.T) {
	// The race detector (make test-race) is the real check; this exercises the
	// locking under contention so a missing lock surfaces.
	s := NewMemorySnapshotStore()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				path := "p" + strconv.Itoa((g+i)%10) + ".go"
				tag := s.Record(path, "g"+strconv.Itoa(g)+"_i"+strconv.Itoa(i)+"\n")
				s.ByHash(path, tag)
				s.Head(path)
				if i%50 == 0 {
					s.Invalidate(path)
				}
			}
		}(g)
	}
	wg.Wait()
}
