package session

import (
	"context"
	"sync"
	"testing"

	"github.com/Viking602/venat/message"
)

func TestSession_EnsureMainIdempotent(t *testing.T) {
	sess := New(NewMemory())
	ctx := context.Background()
	if err := sess.EnsureMain(ctx, LaneConfiguration{Model: "first"}); err != nil {
		t.Fatal(err)
	}
	_, err := sess.Commit(ctx, []Write{
		InsertEntry{Entry: Entry{ID: "leaf-a", Type: EntryMessage, Message: message.NewText(message.RoleUser, "hi")}},
		SetRegister{Namespace: NSLaneLeaf, Key: "main", Value: mustMarshal("leaf-a")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.EnsureMain(ctx, LaneConfiguration{Model: "second"}); err != nil {
		t.Fatal(err)
	}
	reg, ok, err := sess.Storage().GetRegister(ctx, NSLaneLeaf, "main")
	if err != nil || !ok {
		t.Fatalf("leaf register ok=%v err=%v", ok, err)
	}
	leaf, err := UnmarshalRegister[string](reg.Value)
	if err != nil || leaf != "leaf-a" {
		t.Fatalf("leaf = %q (%v), want leaf-a", leaf, err)
	}
}

func TestSession_EnsureMainConcurrent(t *testing.T) {
	store := NewMemory()
	sessions := []*Session{New(store), New(store)}
	errs := make([]error, len(sessions))
	var wg sync.WaitGroup
	for index, current := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[index] = current.EnsureMain(context.Background(), LaneConfiguration{Model: "shared"})
		}()
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("EnsureMain[%d]() error = %v", index, err)
		}
	}
	for _, namespace := range []string{NSLaneLeaf, NSLaneConfig, NSLaneState} {
		registers, err := store.ListRegisters(context.Background(), namespace, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(registers) != 1 {
			t.Fatalf("%s registers = %#v, want one", namespace, registers)
		}
	}
}

func TestSession_ContextMessagesDropsErrorAssistant(t *testing.T) {
	sess := New(NewMemory())
	ctx := context.Background()
	_, err := sess.Commit(ctx, []Write{
		InsertEntry{Entry: Entry{
			ID:      "u1",
			Type:    EntryMessage,
			Message: message.NewText(message.RoleUser, "hi"),
		}},
		InsertEntry{Entry: Entry{
			ID:         "a1",
			ParentID:   "u1",
			Type:       EntryMessage,
			Message:    message.NewText(message.RoleAssistant, "boom"),
			StopReason: "error",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := sess.ContextMessages(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != message.RoleUser || got[0].Text != "hi" {
		t.Fatalf("ContextMessages = %#v, want only user hi", got)
	}
}

func mustMarshal(v any) []byte {
	raw, err := MarshalRegister(v)
	if err != nil {
		panic(err)
	}
	return raw
}
