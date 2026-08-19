package hashline

import (
	"sync"
	"time"
)

// SnapshotStore records file versions for stale-anchor verification and
// remapping. A no-op/lazy implementation still supports strict stale rejection
// because the patcher reads live files and compares tags directly. The bounded
// per-path history implementation retains the exact bases needed for recovery.
type SnapshotStore interface {
	// Head returns the most recently recorded snapshot for path.
	Head(path string) (Snapshot, bool)
	// ByHash returns the snapshot for path with the given hash, if known.
	// When distinct contents collide on the 16-bit tag, the most recently
	// recorded version with that tag is returned; callers that need an
	// unambiguous base must use UniqueByHash.
	ByHash(path, hash string) (Snapshot, bool)
	// UniqueByHash returns the snapshot for path with the given hash only when
	// it is the single distinct content recorded under that tag. It reports
	// false when nothing is recorded under the tag AND when two or more
	// distinct contents collide on it (an ambiguous 16-bit handle that cannot
	// identify which version an edit was built on). The patcher takes its
	// fast path / recovers only against such an unambiguous base.
	UniqueByHash(path, hash string) (Snapshot, bool)
	// Record stores fullText for path and returns its computed tag.
	Record(path, fullText string) string
	// Invalidate forgets all snapshots for path.
	Invalidate(path string)
	// Clear forgets every snapshot.
	Clear()
}

// LazySnapshotStore is the M1–M5 no-op store. It records nothing and
// reports no history, which is exactly what the stale-reject path needs:
// the patcher compares the section tag against the live file directly, so
// the store is never consulted to authorize an apply. Record still
// computes and returns the tag so callers can use it as a tag minter
// without branching on a nil store.
//
// It is safe to use the zero value; all methods are no-ops aside from
// Record's pure computation.
type LazySnapshotStore struct{}

// Head always reports no recorded head.
func (LazySnapshotStore) Head(string) (Snapshot, bool) { return Snapshot{}, false }

// ByHash always reports no match.
func (LazySnapshotStore) ByHash(string, string) (Snapshot, bool) { return Snapshot{}, false }

// UniqueByHash always reports no unique base: the lazy store retains nothing.
func (LazySnapshotStore) UniqueByHash(string, string) (Snapshot, bool) { return Snapshot{}, false }

// Record computes and returns the tag for fullText without retaining it.
func (LazySnapshotStore) Record(path, fullText string) string {
	return ComputeFileHash(Normalize(fullText).Text)
}

// Invalidate is a no-op.
func (LazySnapshotStore) Invalidate(string) {}

// Clear is a no-op.
func (LazySnapshotStore) Clear() {}

// newSnapshot is a small constructor used when a future store wants to
// materialize a Snapshot from full text; kept here so the timestamp source
// is centralized.
func newSnapshot(path, fullText string) Snapshot {
	text := Normalize(fullText).Text
	return Snapshot{
		Path:       path,
		Text:       text,
		Hash:       ComputeFileHash(text),
		RecordedAt: time.Now(),
	}
}

// Store bounds for MemorySnapshotStore (spec §4.8). These cap memory growth:
// at most maxPaths distinct files are tracked, each retaining its
// maxVersionsPerPath most recent distinct versions. When a new path would
// exceed maxPaths, the least-recently-used path is evicted whole.
const (
	maxPaths           = 64
	maxVersionsPerPath = 8
)

// pathHistory is the bounded per-path version ring. versions holds distinct
// recorded snapshots oldest-first; the last element is the head (the most
// recently recorded). byHash indexes versions by tag: because the tag is only
// the low 16 bits of FNV, distinct contents can collide, so each tag maps to
// the slice of version indices sharing it (oldest-first). seq is the global
// recency stamp of the most recent Record for this path, used for cross-path
// LRU eviction.
type pathHistory struct {
	versions []Snapshot
	byHash   map[string][]int
	seq      uint64
}

// MemorySnapshotStore is the in-memory history backing stale-anchor recovery
// and ByHash. It keeps a bounded per-path version history
// (maxVersionsPerPath most recent distinct versions) across at most maxPaths
// files, evicting the least-recently-used path when full. It is safe for
// concurrent use; every method takes the mutex.
//
// The zero value is ready to use.
type MemorySnapshotStore struct {
	mu      sync.Mutex
	paths   map[string]*pathHistory
	counter uint64 // monotonic recency clock
}

// NewMemorySnapshotStore returns an empty, ready-to-use history store.
func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{paths: make(map[string]*pathHistory)}
}

// ensureInit lazily initializes the paths map so the zero value works.
// Callers must hold the mutex.
func (s *MemorySnapshotStore) ensureInit() {
	if s.paths == nil {
		s.paths = make(map[string]*pathHistory)
	}
}

// tick returns the next monotonic recency stamp. Callers must hold the mutex.
func (s *MemorySnapshotStore) tick() uint64 {
	s.counter++
	return s.counter
}

// Record stores fullText for path and returns its computed tag. The text is
// normalized (LF, BOM-stripped) before hashing and storage. Recording content
// that exactly equals a retained version does not duplicate it: that version is
// promoted to head and its tag returned. Distinct content that merely collides
// on the 16-bit tag with a retained version is kept as a separate version, so a
// colliding out-of-band file can never masquerade as the version a caller read.
// Recording bumps the path's recency so it is the last to be evicted.
func (s *MemorySnapshotStore) Record(path, fullText string) string {
	snap := newSnapshot(path, fullText)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureInit()

	h := s.paths[path]
	if h == nil {
		// Evict the LRU path before admitting a new one if we are at capacity.
		if len(s.paths) >= maxPaths {
			s.evictLRULocked()
		}
		h = &pathHistory{byHash: make(map[string][]int)}
		s.paths[path] = h
	}

	// Look for an existing version with the SAME content (not merely the same
	// tag): only exact-text matches are deduplicated; a tag collision keeps both.
	idx := -1
	for _, i := range h.byHash[snap.Hash] {
		if h.versions[i].Text == snap.Text {
			idx = i
			break
		}
	}
	if idx >= 0 {
		// Same content already recorded: promote it to head so it remains the
		// most recent version (and survives version-count eviction longest).
		existing := h.versions[idx]
		existing.RecordedAt = snap.RecordedAt
		h.versions = append(h.versions[:idx], h.versions[idx+1:]...)
		h.versions = append(h.versions, existing)
	} else {
		h.versions = append(h.versions, snap)
		// Trim oldest versions beyond the per-path cap.
		if len(h.versions) > maxVersionsPerPath {
			h.versions = h.versions[len(h.versions)-maxVersionsPerPath:]
		}
	}
	s.reindexLocked(h)

	h.seq = s.tick()
	return snap.Hash
}

// reindexLocked rebuilds h.byHash from h.versions. Callers must hold the
// mutex. Each tag maps to every version index sharing it: distinct contents can
// collide on the 16-bit tag, so colliding versions are retained side by side
// rather than collapsed. Versions are appended chronologically, so each tag's
// index slice is oldest-first and its last entry is the most recent version
// with that tag.
func (s *MemorySnapshotStore) reindexLocked(h *pathHistory) {
	h.byHash = make(map[string][]int, len(h.versions))
	for i, v := range h.versions {
		h.byHash[v.Hash] = append(h.byHash[v.Hash], i)
	}
}

// evictLRULocked removes the path with the smallest recency stamp. Callers
// must hold the mutex and must have at least one path.
func (s *MemorySnapshotStore) evictLRULocked() {
	var victim string
	var lowest uint64
	first := true
	for p, h := range s.paths {
		if first || h.seq < lowest {
			victim = p
			lowest = h.seq
			first = false
		}
	}
	delete(s.paths, victim)
}

// Head returns the most recently recorded snapshot for path.
func (s *MemorySnapshotStore) Head(path string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.paths[path]
	if h == nil || len(h.versions) == 0 {
		return Snapshot{}, false
	}
	return h.versions[len(h.versions)-1], true
}

// ByHash returns the historical snapshot for path whose tag equals hash, if
// it is still retained. When distinct contents collide on the tag, the most
// recently recorded colliding version is returned (its slice is oldest-first,
// so the last index is newest). A read does not change recency (only Record
// does), so looking up an old version never rescues it from version-count
// eviction.
func (s *MemorySnapshotStore) ByHash(path, hash string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.paths[path]
	if h == nil {
		return Snapshot{}, false
	}
	idxs, ok := h.byHash[hash]
	if !ok || len(idxs) == 0 {
		return Snapshot{}, false
	}
	return h.versions[idxs[len(idxs)-1]], true
}

// UniqueByHash returns the snapshot for path with the given hash only when it
// is the sole distinct content recorded under that tag. Record deduplicates by
// exact content, so each index under a tag is a distinct version; a tag with
// exactly one index is therefore unambiguous, while zero (unknown) or two or
// more (a 16-bit collision) are not. In the ambiguous case the tag cannot
// identify which version an edit was built on, so it reports false.
func (s *MemorySnapshotStore) UniqueByHash(path, hash string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.paths[path]
	if h == nil {
		return Snapshot{}, false
	}
	idxs := h.byHash[hash]
	if len(idxs) != 1 {
		return Snapshot{}, false
	}
	return h.versions[idxs[0]], true
}

// Invalidate forgets all snapshots for path.
func (s *MemorySnapshotStore) Invalidate(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.paths, path)
}

// Clear forgets every snapshot.
func (s *MemorySnapshotStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = make(map[string]*pathHistory)
	s.counter = 0
}

// Ensure MemorySnapshotStore satisfies the interface.
var _ SnapshotStore = (*MemorySnapshotStore)(nil)
