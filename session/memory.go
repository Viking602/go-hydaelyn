package session

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/venat/message"
)

var allowedNamespaces = map[string]struct{}{
	NSLaneLeaf:       {},
	NSLaneConfig:     {},
	NSLaneState:      {},
	NSLaneLastResult: {},
	NSOpMeta:         {},
	NSOpState:        {},
}

type memoryState struct {
	entries   map[string]Entry
	registers map[string]Register
	usage     map[string]UsageRow
	nextSeq   int64
}

// Memory is a process-local Storage implementation.
type Memory struct {
	mu     sync.Mutex
	closed bool
	state  memoryState
}

func NewMemory() *Memory {
	return &Memory{state: newMemoryState()}
}

func newMemoryState() memoryState {
	return memoryState{
		entries:   make(map[string]Entry),
		registers: make(map[string]Register),
		usage:     make(map[string]UsageRow),
		nextSeq:   1,
	}
}

func cloneMemoryState(src memoryState) memoryState {
	cloned := newMemoryState()
	cloned.nextSeq = src.nextSeq
	for id, entry := range src.entries {
		cloned.entries[id] = cloneEntry(entry)
	}
	for key, reg := range src.registers {
		cloned.registers[key] = cloneRegister(reg)
	}
	for id, row := range src.usage {
		cloned.usage[id] = row
	}
	return cloned
}

func registerKey(namespace, key string) string {
	return namespace + "\x00" + key
}

func registerPrecondition(registers map[string]Register, writeNamespace, writeKey string, compare bool, expected int64) bool {
	if !compare {
		return true
	}
	current, ok := registers[registerKey(writeNamespace, writeKey)]
	if expected == 0 {
		return !ok
	}
	return ok && current.Seq == expected
}

// Every read and write honors ctx before touching state. A real backend fails
// a cancelled call at the transport, and the harness relies on that: a
// finalization path that forgets to detach its context must fail loudly here
// rather than silently persisting through a dead context.
func (m *Memory) Commit(ctx context.Context, writes []Write) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return CommitResult{}, ErrClosed
	}
	if len(writes) == 0 {
		return CommitResult{}, nil
	}
	clone := cloneMemoryState(m.state)
	now := time.Now().UnixMilli()
	seqs := make([]int64, 0, len(writes))
	for _, write := range writes {
		seq, err := applyWrite(&clone, write, now)
		if err != nil {
			return CommitResult{}, err
		}
		seqs = append(seqs, seq)
	}
	m.state = clone
	return CommitResult{FirstSeq: seqs[0], Seqs: seqs, Timestamp: now}, nil
}

func applyWrite(state *memoryState, write Write, now int64) (int64, error) {
	var err error
	switch current := write.(type) {
	case InsertEntry:
		err = applyEntryWrite(state, current, now)
	case InsertUsage:
		err = applyUsageWrite(state, current, now)
	case SetRegister:
		err = applyRegisterWrite(state, current)
	case DeleteRegister:
		err = applyRegisterDelete(state, current)
	default:
		err = ErrInvalidWrite
	}
	if err != nil {
		return 0, err
	}
	seq := state.nextSeq
	state.nextSeq++
	return seq, nil
}

func applyEntryWrite(state *memoryState, write InsertEntry, now int64) error {
	if write.Entry.ID == "" || write.Entry.Type != EntryMessage {
		return ErrInvalidWrite
	}
	if idTaken(*state, write.Entry.ID) {
		return ErrDuplicateID
	}
	if write.Entry.ParentID != "" {
		if _, ok := state.entries[write.Entry.ParentID]; !ok {
			return ErrInvalidWrite
		}
	}
	entry := cloneEntry(write.Entry)
	entry.Seq = state.nextSeq
	entry.Timestamp = now
	state.entries[entry.ID] = entry
	return nil
}

func applyUsageWrite(state *memoryState, write InsertUsage, now int64) error {
	if write.Row.ID == "" || write.Row.EntryID == "" {
		return ErrInvalidWrite
	}
	if idTaken(*state, write.Row.ID) {
		return ErrDuplicateID
	}
	if _, ok := state.entries[write.Row.EntryID]; !ok {
		return ErrInvalidWrite
	}
	row := write.Row
	row.Seq = state.nextSeq
	row.Timestamp = now
	state.usage[row.ID] = row
	return nil
}

func applyRegisterWrite(state *memoryState, write SetRegister) error {
	if _, ok := allowedNamespaces[write.Namespace]; !ok || write.Key == "" ||
		write.ExpectedSeq < 0 || !json.Valid(write.Value) {
		return ErrInvalidWrite
	}
	if !registerPrecondition(state.registers, write.Namespace, write.Key, write.CompareSeq, write.ExpectedSeq) {
		return ErrConflict
	}
	state.registers[registerKey(write.Namespace, write.Key)] = Register{
		Namespace: write.Namespace,
		Key:       write.Key,
		Value:     slices.Clone(write.Value),
		Seq:       state.nextSeq,
	}
	return nil
}

func applyRegisterDelete(state *memoryState, write DeleteRegister) error {
	if _, ok := allowedNamespaces[write.Namespace]; !ok || write.Key == "" || write.ExpectedSeq < 0 {
		return ErrInvalidWrite
	}
	if !registerPrecondition(state.registers, write.Namespace, write.Key, write.CompareSeq, write.ExpectedSeq) {
		return ErrConflict
	}
	delete(state.registers, registerKey(write.Namespace, write.Key))
	return nil
}

func idTaken(state memoryState, id string) bool {
	if _, ok := state.entries[id]; ok {
		return true
	}
	_, ok := state.usage[id]
	return ok
}

func (m *Memory) GetEntries(ctx context.Context, ids []string) (map[string]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	out := make(map[string]Entry, len(ids))
	for _, id := range ids {
		if entry, ok := m.state.entries[id]; ok {
			out[id] = cloneEntry(entry)
		}
	}
	return out, nil
}

func (m *Memory) GetUsage(ctx context.Context, ids []string) (map[string]UsageRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	out := make(map[string]UsageRow, len(ids))
	for _, id := range ids {
		if row, ok := m.state.usage[id]; ok {
			out[id] = row
		}
	}
	return out, nil
}

func (m *Memory) GetRegister(ctx context.Context, namespace, key string) (Register, bool, error) {
	if err := ctx.Err(); err != nil {
		return Register{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Register{}, false, ErrClosed
	}
	reg, ok := m.state.registers[registerKey(namespace, key)]
	return cloneRegister(reg), ok, nil
}

func (m *Memory) ListRegisters(ctx context.Context, namespace, keyPrefix string) ([]Register, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	var out []Register
	for _, reg := range m.state.registers {
		if reg.Namespace == namespace && strings.HasPrefix(reg.Key, keyPrefix) {
			out = append(out, cloneRegister(reg))
		}
	}
	slices.SortFunc(out, func(a, b Register) int {
		return strings.Compare(a.Key, b.Key)
	})
	return out, nil
}

func (m *Memory) ScanBranch(ctx context.Context, startID string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if startID == "" {
		return nil, nil
	}
	entry, ok := m.state.entries[startID]
	if !ok {
		return nil, ErrNotFound
	}
	visited := make(map[string]struct{})
	var chain []Entry
	for {
		if _, seen := visited[entry.ID]; seen {
			return nil, ErrCorrupt
		}
		visited[entry.ID] = struct{}{}
		chain = append(chain, cloneEntry(entry))
		if entry.ParentID == "" {
			break
		}
		parent, found := m.state.entries[entry.ParentID]
		if !found {
			return nil, ErrCorrupt
		}
		entry = parent
	}
	slices.Reverse(chain)
	return chain, nil
}

func (m *Memory) Close(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func cloneEntry(entry Entry) Entry {
	entry.Message = message.Clone(entry.Message)
	return entry
}

func cloneRegister(reg Register) Register {
	reg.Value = slices.Clone(reg.Value)
	return reg
}
