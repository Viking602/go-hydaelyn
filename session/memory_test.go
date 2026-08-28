package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
)

func TestMemory_EmptyCommit(t *testing.T) {
	store := NewMemory()
	got, err := store.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got.FirstSeq != 0 || got.Timestamp != 0 || len(got.Seqs) != 0 {
		t.Fatalf("empty commit = %#v, want zero", got)
	}
}

func TestMemory_InsertAndSetAtomic(t *testing.T) {
	store := NewMemory()
	raw, err := json.Marshal("leaf-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "e1", Type: EntryMessage, Message: message.NewText(message.RoleUser, "hi")}},
		SetRegister{Namespace: NSLaneLeaf, Key: "main", Value: raw},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	entries, err := store.GetEntries(context.Background(), []string{"e1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["e1"]; !ok {
		t.Fatal("entry missing after commit")
	}
	reg, ok, err := store.GetRegister(context.Background(), NSLaneLeaf, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("register missing after commit")
	}
	var leaf string
	if err := json.Unmarshal(reg.Value, &leaf); err != nil || leaf != "leaf-1" {
		t.Fatalf("register = %s, want leaf-1 (%v)", reg.Value, err)
	}
}

func TestMemory_CommitRollsBackEveryWriteOnFailure(t *testing.T) {
	store := NewMemory()
	_, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "rolled-back", Type: EntryMessage}},
		SetRegister{Namespace: NSLaneLeaf, Key: "main", Value: json.RawMessage(`{`)},
	})
	if !errors.Is(err, ErrInvalidWrite) {
		t.Fatalf("Commit() error = %v, want ErrInvalidWrite", err)
	}
	entries, err := store.GetEntries(context.Background(), []string{"rolled-back"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed commit persisted entries: %#v", entries)
	}
	result, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "first", Type: EntryMessage}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FirstSeq != 1 {
		t.Fatalf("first successful sequence = %d, want 1", result.FirstSeq)
	}
}

func TestMemory_RegisterCompareAndSwap(t *testing.T) {
	store := NewMemory()
	if _, err := store.Commit(context.Background(), []Write{
		SetRegister{Namespace: NSLaneState, Key: "main", Value: json.RawMessage(`{"currentOperationId":"one"}`), CompareSeq: true},
	}); err != nil {
		t.Fatal(err)
	}
	first, ok, err := store.GetRegister(context.Background(), NSLaneState, "main")
	if err != nil || !ok {
		t.Fatalf("GetRegister() ok=%t error=%v", ok, err)
	}
	if _, err := store.Commit(context.Background(), []Write{
		SetRegister{
			Namespace: NSLaneState, Key: "main", Value: json.RawMessage(`{"currentOperationId":"two"}`),
			CompareSeq: true, ExpectedSeq: first.Seq,
		},
	}); err != nil {
		t.Fatalf("fresh compare-and-swap error = %v", err)
	}
	if _, err := store.Commit(context.Background(), []Write{
		SetRegister{
			Namespace: NSLaneState, Key: "main", Value: json.RawMessage(`{"currentOperationId":"stale"}`),
			CompareSeq: true, ExpectedSeq: first.Seq,
		},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale compare-and-swap error = %v, want ErrConflict", err)
	}
}

func TestMemory_RejectsMalformedWrites(t *testing.T) {
	tests := []struct {
		name  string
		write Write
	}{
		{name: "malformed register JSON", write: SetRegister{Namespace: NSLaneState, Key: "main", Value: json.RawMessage(`{`)}},
		{name: "unknown delete namespace", write: DeleteRegister{Namespace: "future", Key: "main"}},
		{name: "orphan usage", write: InsertUsage{Row: UsageRow{ID: "usage", EntryID: "missing"}}},
		{name: "negative expected sequence", write: SetRegister{
			Namespace: NSLaneState, Key: "main", Value: json.RawMessage(`{}`), CompareSeq: true, ExpectedSeq: -1,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewMemory().Commit(context.Background(), []Write{test.write}); !errors.Is(err, ErrInvalidWrite) {
				t.Fatalf("Commit() error = %v, want ErrInvalidWrite", err)
			}
		})
	}
}

func TestMemory_EntriesAreDeepCopied(t *testing.T) {
	store := NewMemory()
	msg := message.NewText(message.RoleUser, "hello")
	msg.Metadata = map[string]string{"owner": "caller"}
	msg.ProviderState = json.RawMessage(`{"cursor":"one"}`)
	msg.ToolCalls = []message.ToolCall{{ID: "call", Name: "lookup", Arguments: json.RawMessage(`{"q":"one"}`)}}
	if _, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "entry", Type: EntryMessage, Message: msg}},
	}); err != nil {
		t.Fatal(err)
	}
	msg.Metadata["owner"] = "mutated"
	msg.ProviderState[0] = '['
	msg.ToolCalls[0].Arguments[0] = '['

	first, err := store.GetEntries(context.Background(), []string{"entry"})
	if err != nil {
		t.Fatal(err)
	}
	if first["entry"].Message.Metadata["owner"] != "caller" ||
		string(first["entry"].Message.ProviderState) != `{"cursor":"one"}` ||
		string(first["entry"].Message.ToolCalls[0].Arguments) != `{"q":"one"}` {
		t.Fatalf("stored entry aliased caller data: %#v", first["entry"])
	}
	firstEntry := first["entry"]
	firstEntry.Message.Metadata["owner"] = "reader"
	second, err := store.GetEntries(context.Background(), []string{"entry"})
	if err != nil {
		t.Fatal(err)
	}
	if second["entry"].Message.Metadata["owner"] != "caller" {
		t.Fatalf("stored entry aliased read result: %#v", second["entry"])
	}
}

func TestMemory_DuplicateID(t *testing.T) {
	store := NewMemory()
	entry := InsertEntry{Entry: Entry{ID: "dup", Type: EntryMessage}}
	if _, err := store.Commit(context.Background(), []Write{entry}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := store.Commit(context.Background(), []Write{entry}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("second insert error = %v, want ErrDuplicateID", err)
	}
}

func TestMemory_DeleteAbsentRegister(t *testing.T) {
	store := NewMemory()
	if _, err := store.Commit(context.Background(), []Write{
		DeleteRegister{Namespace: NSLaneLeaf, Key: "missing"},
	}); err != nil {
		t.Fatalf("delete absent: %v", err)
	}
}

func TestMemory_ScanBranchOldestFirst(t *testing.T) {
	store := NewMemory()
	_, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "a", Type: EntryMessage, Message: message.NewText(message.RoleUser, "a")}},
		InsertEntry{Entry: Entry{ID: "b", ParentID: "a", Type: EntryMessage, Message: message.NewText(message.RoleUser, "b")}},
		InsertEntry{Entry: Entry{ID: "c", ParentID: "b", Type: EntryMessage, Message: message.NewText(message.RoleUser, "c")}},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	got, err := store.ScanBranch(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("ScanBranch = %#v, want a→b→c", idsOf(got))
	}
}

func TestMemory_ScanBranchRejectsMissingAndCyclicTrees(t *testing.T) {
	store := NewMemory()
	if _, err := store.ScanBranch(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ScanBranch(missing) error = %v, want ErrNotFound", err)
	}
	store.mu.Lock()
	store.state.entries["a"] = Entry{ID: "a", ParentID: "b", Type: EntryMessage}
	store.state.entries["b"] = Entry{ID: "b", ParentID: "a", Type: EntryMessage}
	store.mu.Unlock()
	if _, err := store.ScanBranch(context.Background(), "a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ScanBranch(cycle) error = %v, want ErrCorrupt", err)
	}
}

func TestMemory_CloseThenCommit(t *testing.T) {
	store := NewMemory()
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "e", Type: EntryMessage}},
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Commit after Close = %v, want ErrClosed", err)
	}
	if _, err := store.GetUsage(context.Background(), []string{"usage"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetUsage after Close = %v, want ErrClosed", err)
	}
}

func idsOf(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.ID
	}
	return out
}

func TestMemory_CancelledContext(t *testing.T) {
	store := NewMemory()
	if _, err := store.Commit(context.Background(), []Write{
		InsertEntry{Entry: Entry{ID: "e1", Type: EntryMessage, Message: message.NewText(message.RoleUser, "hi")}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		call func(context.Context) error
	}{
		{name: "Commit", call: func(ctx context.Context) error {
			_, err := store.Commit(ctx, []Write{DeleteRegister{Namespace: NSLaneLeaf, Key: "main"}})
			return err
		}},
		{name: "GetEntries", call: func(ctx context.Context) error {
			_, err := store.GetEntries(ctx, []string{"e1"})
			return err
		}},
		{name: "GetUsage", call: func(ctx context.Context) error {
			_, err := store.GetUsage(ctx, []string{"usage"})
			return err
		}},
		{name: "GetRegister", call: func(ctx context.Context) error {
			_, _, err := store.GetRegister(ctx, NSLaneLeaf, "main")
			return err
		}},
		{name: "ListRegisters", call: func(ctx context.Context) error {
			_, err := store.ListRegisters(ctx, NSLaneLeaf, "")
			return err
		}},
		{name: "ScanBranch", call: func(ctx context.Context) error {
			_, err := store.ScanBranch(ctx, "e1")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s on a cancelled context = %v, want context.Canceled", tt.name, err)
			}
		})
	}
	// Close releases process-local state and must not depend on the caller's
	// context still being live.
	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close on a cancelled context = %v", err)
	}
}
