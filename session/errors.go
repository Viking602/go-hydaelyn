package session

import "errors"

var (
	ErrClosed       = errors.New("session: closed")
	ErrDuplicateID  = errors.New("session: duplicate id")
	ErrCorrupt      = errors.New("session: corrupt")
	ErrInvalidWrite = errors.New("session: invalid write")
	ErrConflict     = errors.New("session: write conflict")
	ErrNotFound     = errors.New("session: not found")
)
