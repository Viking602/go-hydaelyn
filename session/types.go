package session

import (
	"encoding/json"

	"github.com/Viking602/venat/message"
)

type EntryType string

const EntryMessage EntryType = "message"

type Entry struct {
	ID           string
	ParentID     string // empty = root
	Seq          int64  // storage-assigned
	Timestamp    int64  // unix ms, storage-assigned, same value for every write in one Commit
	Type         EntryType
	Message      message.Message
	StopReason   string // provider.StopReason value; empty if not an assistant settlement
	ErrorMessage string
}

type UsageRow struct {
	ID        string
	Seq       int64
	Timestamp int64
	EntryID   string
	Usage     Usage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

const (
	NSLaneLeaf       = "lane.leaf"
	NSLaneConfig     = "lane.config"
	NSLaneState      = "lane.state"
	NSLaneLastResult = "lane.lastResult"
	NSOpMeta         = "op.meta"
	NSOpState        = "op.state"
)

type LaneConfiguration struct {
	Model string `json:"model"`
}

type LaneState struct {
	CurrentOperationID string `json:"currentOperationId"`
	OwnerID            string `json:"ownerId,omitempty"`
	LeaseExpiresAt     int64  `json:"leaseExpiresAt,omitempty"`
}

type LaneLastResult struct {
	OperationID           string          `json:"operationId"`
	Kind                  string          `json:"kind"` // always "run" this slice
	LeafID                string          `json:"leafId"`
	FinalAssistantEntryID string          `json:"finalAssistantEntryId,omitempty"`
	Outcome               string          `json:"outcome"` // completed | failed
	Error                 *OperationError `json:"error,omitempty"`
}

type OperationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Register struct {
	Namespace string
	Key       string
	Value     json.RawMessage
	Seq       int64
}
