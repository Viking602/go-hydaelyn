package session

import (
	"context"
	"encoding/json"
	"errors"
)

type Session struct {
	store Storage
	ids   *IDGenerator
}

func New(store Storage) *Session {
	return &Session{store: store, ids: NewIDGenerator()}
}

func (s *Session) IDs() *IDGenerator { return s.ids }

func (s *Session) Storage() Storage { return s.store }

func (s *Session) Close(ctx context.Context) error {
	return s.store.Close(ctx)
}

func (s *Session) Commit(ctx context.Context, writes []Write) (CommitResult, error) {
	normalized := make([]Write, len(writes))
	for i, write := range writes {
		switch w := write.(type) {
		case SetRegister:
			if w.Namespace == "" || w.Key == "" || w.ExpectedSeq < 0 {
				return CommitResult{}, ErrInvalidWrite
			}
			var raw json.RawMessage
			if err := json.Unmarshal(w.Value, &raw); err != nil {
				return CommitResult{}, ErrInvalidWrite
			}
			w.Value = raw
			normalized[i] = w
		case DeleteRegister:
			if w.Namespace == "" || w.Key == "" || w.ExpectedSeq < 0 {
				return CommitResult{}, ErrInvalidWrite
			}
			normalized[i] = w
		default:
			normalized[i] = write
		}
	}
	return s.store.Commit(ctx, normalized)
}

func MarshalRegister(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

func UnmarshalRegister[T any](raw json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(raw, &value)
	return value, err
}

func (s *Session) EnsureMain(ctx context.Context, cfg LaneConfiguration) error {
	_, ok, err := s.store.GetRegister(ctx, NSLaneState, "main")
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	configRaw, err := MarshalRegister(cfg)
	if err != nil {
		return err
	}
	stateRaw, err := MarshalRegister(LaneState{})
	if err != nil {
		return err
	}
	_, err = s.Commit(ctx, []Write{
		SetRegister{Namespace: NSLaneLeaf, Key: "main", Value: json.RawMessage("null"), CompareSeq: true},
		SetRegister{Namespace: NSLaneConfig, Key: "main", Value: configRaw, CompareSeq: true},
		SetRegister{Namespace: NSLaneState, Key: "main", Value: stateRaw, CompareSeq: true},
	})
	if !errors.Is(err, ErrConflict) {
		return err
	}
	_, ok, readErr := s.store.GetRegister(ctx, NSLaneState, "main")
	if readErr != nil {
		return readErr
	}
	if !ok {
		return ErrCorrupt
	}
	return nil
}
