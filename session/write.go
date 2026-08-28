package session

import "encoding/json"

type Write interface{ isWrite() }

type InsertEntry struct{ Entry Entry }

func (InsertEntry) isWrite() {}

type InsertUsage struct{ Row UsageRow }

func (InsertUsage) isWrite() {}

// SetRegister replaces one register. When CompareSeq is true, Commit succeeds
// only if the current register sequence equals ExpectedSeq; ExpectedSeq zero
// means the register must not exist.
type SetRegister struct {
	Namespace   string
	Key         string
	Value       json.RawMessage
	CompareSeq  bool
	ExpectedSeq int64
}

func (SetRegister) isWrite() {}

// DeleteRegister removes one register. CompareSeq has the same semantics as
// SetRegister.
type DeleteRegister struct {
	Namespace   string
	Key         string
	CompareSeq  bool
	ExpectedSeq int64
}

func (DeleteRegister) isWrite() {}

type CommitResult struct {
	FirstSeq  int64
	Seqs      []int64
	Timestamp int64
}
