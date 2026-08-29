package durable

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Viking602/venat/agent"
)

// HashExecutionSpec returns the canonical JSON SHA-256 of spec.
func HashExecutionSpec(spec ExecutionSpec) ([32]byte, error) {
	return canonicalJSONHash(spec)
}

// HashContinuation returns SHA-256 over agent.EncodeContinuation's canonical
// bytes.
func HashContinuation(continuation agent.Continuation) ([32]byte, error) {
	encoded, err := agent.EncodeContinuation(continuation)
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode continuation: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

// ValidateCheckpoint verifies sequence, continuation integrity, schema version,
// and canonical continuation hash.
func ValidateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.Sequence == 0 {
		return fmt.Errorf("%w: checkpoint sequence is zero", ErrCorruptCheckpoint)
	}
	hash, err := HashContinuation(checkpoint.Continuation)
	if err != nil {
		return errors.Join(ErrCorruptCheckpoint, err)
	}
	if hash != checkpoint.ContinuationHash {
		return fmt.Errorf("%w: continuation hash mismatch", ErrCorruptCheckpoint)
	}
	return nil
}

// HashResult returns the canonical JSON SHA-256 of result.
func HashResult(result agent.Result) ([32]byte, error) {
	return canonicalJSONHash(result)
}

func canonicalJSONHash(value any) ([32]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, fmt.Errorf("canonical JSON encode: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return [32]byte{}, fmt.Errorf("canonical JSON decode: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return [32]byte{}, err
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return [32]byte{}, fmt.Errorf("canonical JSON normalize: %w", err)
	}
	return sha256.Sum256(canonical), nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("canonical JSON contains trailing data")
		}
		return fmt.Errorf("canonical JSON trailing data: %w", err)
	}
	return nil
}
