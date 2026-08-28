package session

import "context"

// Storage owns the durable entry tree, usage rows, and registers for a
// Session. Implementations MUST make Commit atomic: either every write and
// sequence assignment is visible or none is. Overlapping commits MUST be
// serialized, and CompareSeq register writes MUST fail with ErrConflict when
// their precondition no longer matches.
//
// Entries and usage rows receive monotonically increasing sequences. All rows
// written by one Commit share its Timestamp. GetEntries and GetUsage return the
// rows they find and omit missing IDs. ScanBranch returns root-to-leaf order;
// a nonempty missing start ID returns ErrNotFound and a broken/cyclic parent
// chain returns ErrCorrupt.
type Storage interface {
	Commit(ctx context.Context, writes []Write) (CommitResult, error)
	GetEntries(ctx context.Context, ids []string) (map[string]Entry, error)
	GetUsage(ctx context.Context, ids []string) (map[string]UsageRow, error)
	GetRegister(ctx context.Context, namespace, key string) (Register, bool, error)
	ListRegisters(ctx context.Context, namespace, keyPrefix string) ([]Register, error)
	ScanBranch(ctx context.Context, startID string) ([]Entry, error)
	Close(ctx context.Context) error
}
