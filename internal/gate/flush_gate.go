// Package gate provides concurrency and ordering primitives for transport
// and messaging layers.
package gate

// FlushGate gates message writes during an initial flush so that new
// messages do not interleave with historical ones.
//
// Lifecycle:
//   - Start()  → Enqueue returns true and items are queued.
//   - End()    → Returns queued items for draining; Enqueue returns false.
//   - Drop()   → Discards queued items (permanent close).
//   - Deactivate() → Clears the active flag without dropping items
//     (useful when the transport is replaced and the new transport will drain).
type FlushGate[T any] struct {
	active  bool
	pending []T
}

// Active reports whether the gate is currently queuing items.
func (g *FlushGate[T]) Active() bool {
	if g == nil {
		return false
	}
	return g.active
}

// PendingCount returns the number of items currently queued.
func (g *FlushGate[T]) PendingCount() int {
	if g == nil {
		return 0
	}
	return len(g.pending)
}

// Start marks a flush as in-progress; Enqueue will begin queuing items.
func (g *FlushGate[T]) Start() {
	if g == nil {
		return
	}
	g.active = true
}

// End finishes the flush and returns any queued items for draining.
// The caller is responsible for sending the returned items.
func (g *FlushGate[T]) End() []T {
	if g == nil {
		return nil
	}
	g.active = false
	items := g.pending
	g.pending = nil
	return items
}

// Enqueue queues items if a flush is active.
// If active, it returns true. Otherwise it returns false and the caller
// should send the items directly.
func (g *FlushGate[T]) Enqueue(items ...T) bool {
	if g == nil || !g.active {
		return false
	}
	g.pending = append(g.pending, items...)
	return true
}

// Drop discards all queued items (e.g. on permanent transport close).
// It returns the number of items dropped.
func (g *FlushGate[T]) Drop() int {
	if g == nil {
		return 0
	}
	g.active = false
	n := len(g.pending)
	g.pending = g.pending[:0]
	return n
}

// Deactivate clears the active flag without dropping items.
// Used when the transport is replaced; the new transport's flush will drain.
func (g *FlushGate[T]) Deactivate() {
	if g == nil {
		return
	}
	g.active = false
}
