package hashline

import "time"

// Snapshot is a recorded view of a file at a point in time. The M1–M5
// patcher operates against live files and does not require a populated
// store, but the type and the SnapshotStore interface (snapshot.go) are
// defined now so the M6 history implementation drops in without changing
// callers.
type Snapshot struct {
	// Path is the workspace-relative file path.
	Path string
	// Text is the LF-normalized, BOM-stripped content at record time.
	Text string
	// Hash is the four-hex-uppercase tag of Text.
	Hash string
	// RecordedAt is when the snapshot was taken.
	RecordedAt time.Time
}

// Patch is a parsed hashline edit covering one or more files. Each Section
// targets a single path/tag pair.
type Patch struct {
	Sections []Section
}

// Section is the set of operations for one file, anchored to the tag the
// model read. All op line numbers reference the original file the tag
// refers to.
type Section struct {
	Path string
	Tag  string
	Ops  []Op
}

// OpKind enumerates the edit operations the first release supports.
type OpKind string

const (
	// OpReplace replaces lines Start..End with Body.
	OpReplace OpKind = "replace"
	// OpDelete removes lines Start..End.
	OpDelete OpKind = "delete"
	// OpInsertBefore inserts Body immediately before line Start.
	OpInsertBefore OpKind = "insert_before"
	// OpInsertAfter inserts Body immediately after line Start.
	OpInsertAfter OpKind = "insert_after"
	// OpInsertHead inserts Body at the very top of the file.
	OpInsertHead OpKind = "insert_head"
	// OpInsertTail inserts Body at the very bottom of the file.
	OpInsertTail OpKind = "insert_tail"
	// OpReplaceBlock replaces the smallest Go syntactic block whose start
	// line is Start with Body. It is resolved to a concrete OpReplace range
	// (via ResolveBlocks) before the line-based applier runs, and is Go-only.
	OpReplaceBlock OpKind = "replace_block"
	// OpDeleteBlock removes the smallest Go syntactic block whose start line
	// is Start. Like OpReplaceBlock it is resolved to a concrete OpDelete
	// range before Apply, and is Go-only.
	OpDeleteBlock OpKind = "delete_block"
)

// Op is a single edit operation. Start and End are 1-based line numbers in
// the original file. For single-line anchors (insert before/after) only
// Start is meaningful; for head/tail neither Start nor End is used. For the
// block ops (OpReplaceBlock/OpDeleteBlock) only Start is meaningful: it is
// the line on which the targeted Go block begins, and End is filled in by
// ResolveBlocks once the block's extent is resolved. Body holds the final
// content lines (without the leading '+').
type Op struct {
	Kind  OpKind
	Start int
	End   int
	Body  []string
}
