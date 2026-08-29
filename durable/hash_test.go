package durable_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/message"
)

func TestCanonicalHashesIgnoreJSONWhitespaceAndMapOrder(t *testing.T) {
	leftSpec := durable.ExecutionSpec{
		Request: agent.Request{Prompt: "hello"},
		OutputPolicy: agent.OutputPolicy{Schema: json.RawMessage(`{
			"properties":{"b":{"type":"string"},"a":{"type":"number"}},
			"type":"object"
		}`)},
	}
	rightSpec := durable.ExecutionSpec{
		Request:      agent.Request{Prompt: "hello"},
		OutputPolicy: agent.OutputPolicy{Schema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"string"}}}`)},
	}
	left, err := durable.HashExecutionSpec(leftSpec)
	if err != nil {
		t.Fatalf("HashExecutionSpec(left) error = %v", err)
	}
	right, err := durable.HashExecutionSpec(rightSpec)
	if err != nil {
		t.Fatalf("HashExecutionSpec(right) error = %v", err)
	}
	if left != right {
		t.Fatalf("canonical hashes differ:\nleft  %x\nright %x", left, right)
	}

	leftContinuation := agent.Continuation{
		SchemaVersion: agent.ContinuationSchemaVersion,
		Messages:      []message.Message{{Role: message.RoleUser, Metadata: map[string]string{"b": "2", "a": "1"}}},
		Phase:         agent.ContinuationReady,
	}
	rightContinuation := agent.Continuation{
		SchemaVersion: agent.ContinuationSchemaVersion,
		Messages:      []message.Message{{Role: message.RoleUser, Metadata: map[string]string{"a": "1", "b": "2"}}},
		Phase:         agent.ContinuationReady,
	}
	left, err = durable.HashContinuation(leftContinuation)
	if err != nil {
		t.Fatalf("HashContinuation(left) error = %v", err)
	}
	right, err = durable.HashContinuation(rightContinuation)
	if err != nil {
		t.Fatalf("HashContinuation(right) error = %v", err)
	}
	if left != right {
		t.Fatalf("continuation hashes differ:\nleft  %x\nright %x", left, right)
	}
}

func TestHashContinuationUsesAgentCanonicalCodec(t *testing.T) {
	continuation := agent.Continuation{
		SchemaVersion: agent.ContinuationSchemaVersion,
		Request:       agent.Request{Prompt: "hello"},
		Messages:      []message.Message{message.NewText(message.RoleUser, "hello")},
		Phase:         agent.ContinuationReady,
	}
	encoded, err := agent.EncodeContinuation(continuation)
	if err != nil {
		t.Fatalf("EncodeContinuation() error = %v", err)
	}
	got, err := durable.HashContinuation(continuation)
	if err != nil {
		t.Fatalf("HashContinuation() error = %v", err)
	}
	if want := sha256.Sum256(encoded); got != want {
		t.Fatalf("HashContinuation() = %x, want SHA-256(codec bytes) %x", got, want)
	}
}

func TestValidateCheckpointRejectsSequenceStateAndHashCorruption(t *testing.T) {
	continuation := agent.Continuation{
		SchemaVersion: agent.ContinuationSchemaVersion,
		Request:       agent.Request{Prompt: "hello"},
		Messages:      []message.Message{message.NewText(message.RoleUser, "hello")},
		Phase:         agent.ContinuationReady,
	}
	hash, err := durable.HashContinuation(continuation)
	if err != nil {
		t.Fatalf("HashContinuation() error = %v", err)
	}
	checkpoint := durable.Checkpoint{Sequence: 1, Continuation: continuation, ContinuationHash: hash}
	if err := durable.ValidateCheckpoint(checkpoint); err != nil {
		t.Fatalf("ValidateCheckpoint() error = %v", err)
	}

	zeroSequence := checkpoint
	zeroSequence.Sequence = 0
	if err := durable.ValidateCheckpoint(zeroSequence); !errors.Is(err, durable.ErrCorruptCheckpoint) {
		t.Fatalf("zero-sequence error = %v, want ErrCorruptCheckpoint", err)
	}

	hashMismatch := checkpoint
	hashMismatch.ContinuationHash[0] ^= 0xff
	if err := durable.ValidateCheckpoint(hashMismatch); !errors.Is(err, durable.ErrCorruptCheckpoint) {
		t.Fatalf("hash-mismatch error = %v, want ErrCorruptCheckpoint", err)
	}

	invalidState := checkpoint
	invalidState.Continuation.SchemaVersion = 0
	if err := durable.ValidateCheckpoint(invalidState); !errors.Is(err, durable.ErrCorruptCheckpoint) || !errors.Is(err, agent.ErrInvalidContinuation) {
		t.Fatalf("invalid-state error = %v, want ErrCorruptCheckpoint and ErrInvalidContinuation", err)
	}
}

func TestHashResultExcludesTransientFailureCause(t *testing.T) {
	left := agent.Result{Failure: (&agent.AgentFailure{Kind: agent.FailureKindEngineError, Reason: "failed"}).WithCause(errors.New("left transient cause"))}
	right := agent.Result{Failure: (&agent.AgentFailure{Kind: agent.FailureKindEngineError, Reason: "failed"}).WithCause(errors.New("right transient cause"))}
	leftHash, err := durable.HashResult(left)
	if err != nil {
		t.Fatalf("HashResult(left) error = %v", err)
	}
	rightHash, err := durable.HashResult(right)
	if err != nil {
		t.Fatalf("HashResult(right) error = %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("transient failure cause changed durable hash:\nleft  %x\nright %x", leftHash, rightHash)
	}
}

func TestHashRejectsInvalidEmbeddedJSON(t *testing.T) {
	_, err := durable.HashExecutionSpec(durable.ExecutionSpec{OutputPolicy: agent.OutputPolicy{Schema: json.RawMessage(`{`)}})
	if err == nil {
		t.Fatal("HashExecutionSpec() error = nil, want invalid JSON error")
	}
}
