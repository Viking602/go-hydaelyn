package gate

import (
	"reflect"
	"testing"
)

func TestFlushGateBasicLifecycle(t *testing.T) {
	g := &FlushGate[string]{}

	if g.Active() {
		t.Error("expected inactive initially")
	}

	g.Start()
	if !g.Active() {
		t.Error("expected active after Start")
	}

	if !g.Enqueue("a", "b") {
		t.Error("expected Enqueue to succeed during active flush")
	}
	if g.PendingCount() != 2 {
		t.Errorf("expected pending count 2, got %d", g.PendingCount())
	}

	pending := g.End()
	if !reflect.DeepEqual(pending, []string{"a", "b"}) {
		t.Errorf("unexpected pending items: %v", pending)
	}
	if g.Active() {
		t.Error("expected inactive after End")
	}
	if g.PendingCount() != 0 {
		t.Errorf("expected 0 pending after End, got %d", g.PendingCount())
	}

	if g.Enqueue("c") {
		t.Error("expected Enqueue to fail when inactive")
	}
}

func TestFlushGateDrop(t *testing.T) {
	g := &FlushGate[int]{}
	g.Start()
	g.Enqueue(1, 2, 3)

	dropped := g.Drop()
	if dropped != 3 {
		t.Errorf("expected 3 dropped, got %d", dropped)
	}
	if g.Active() {
		t.Error("expected inactive after Drop")
	}
	if g.PendingCount() != 0 {
		t.Errorf("expected 0 pending after Drop, got %d", g.PendingCount())
	}
}

func TestFlushGateDeactivatePreservesItems(t *testing.T) {
	g := &FlushGate[string]{}
	g.Start()
	g.Enqueue("x", "y")

	g.Deactivate()
	if g.Active() {
		t.Error("expected inactive after Deactivate")
	}
	if g.PendingCount() != 2 {
		t.Errorf("expected 2 pending after Deactivate, got %d", g.PendingCount())
	}

	// End should still return the preserved items
	pending := g.End()
	if !reflect.DeepEqual(pending, []string{"x", "y"}) {
		t.Errorf("unexpected pending items after End: %v", pending)
	}
}

func TestFlushGateNilSafety(t *testing.T) {
	var g *FlushGate[string]

	if g.Active() {
		t.Error("expected nil gate to be inactive")
	}
	if g.PendingCount() != 0 {
		t.Error("expected nil gate to have 0 pending")
	}
	if g.Enqueue("a") {
		t.Error("expected Enqueue on nil gate to fail")
	}
	if items := g.End(); items != nil {
		t.Errorf("expected End on nil gate to return nil, got %v", items)
	}
	if g.Drop() != 0 {
		t.Error("expected Drop on nil gate to return 0")
	}
	g.Deactivate() // should not panic
	g.Start()      // should not panic
}
