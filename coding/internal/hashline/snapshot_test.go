package hashline

import "testing"

func TestLazySnapshotStore_NoOpBehavior(t *testing.T) {
	var s LazySnapshotStore
	if _, ok := s.Head("x"); ok {
		t.Error("Head should report no head")
	}
	if _, ok := s.ByHash("x", "0000"); ok {
		t.Error("ByHash should report no match")
	}
	if s.ContainsText("x", "anything\n") {
		t.Error("ContainsText should report nothing retained")
	}
	// Invalidate and Clear must not panic.
	s.Invalidate("x")
	s.Clear()
}

func TestLazySnapshotStore_RecordReturnsTag(t *testing.T) {
	var s LazySnapshotStore
	tag := s.Record("x.go", "package x\n")
	if tag != ComputeFileHash("package x\n") {
		t.Errorf("Record tag = %q, want computed tag", tag)
	}
	// Recording must not start reporting a head (it is no-op storage).
	if _, ok := s.Head("x.go"); ok {
		t.Error("LazySnapshotStore must not retain recorded content")
	}
}

func TestLazySnapshotStore_RecordNormalizes(t *testing.T) {
	var s LazySnapshotStore
	bom := "\uFEFF"
	a := s.Record("f", bom+"a\r\nb\r\n")
	b := s.Record("f", "a\nb\n")
	if a != b {
		t.Errorf("Record should normalize before hashing: %q vs %q", a, b)
	}
}

func TestNewSnapshot(t *testing.T) {
	bom := "\uFEFF"
	snap := newSnapshot("f.go", bom+"a\r\nb\r\n")
	if snap.Path != "f.go" {
		t.Errorf("Path = %q", snap.Path)
	}
	if snap.Text != "a\nb\n" {
		t.Errorf("Text = %q, want LF/BOM-free", snap.Text)
	}
	if snap.Hash != ComputeFileHash("a\nb\n") {
		t.Errorf("Hash = %q", snap.Hash)
	}
	if snap.RecordedAt.IsZero() {
		t.Error("RecordedAt should be set")
	}
}
