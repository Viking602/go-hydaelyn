// Package hashline implements the pure-Go core of the hashline
// line-anchored edit protocol: file normalization, the FNV tag
// fingerprint, the numbered read/edit formats, a strict patch parser, an
// original-line-anchored applier, and an all-or-nothing multi-section
// patcher.
//
// The package is pure: it never touches the real disk and never imports a
// framework package. Disk access is abstracted behind the Filesystem
// interface (see patcher.go); the host wires a concrete sandboxed
// implementation. Spec anchor: docs/coding-agent-hashline.md section 4.
package hashline

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the parser, applier, and patcher. Callers
// match these with errors.Is to map a failure onto an agent-facing
// recovery message (e.g. ErrSnapshotMismatch instructs a re-read).
var (
	// ErrParse is the umbrella parse failure; specific parse errors below
	// wrap it so errors.Is(err, ErrParse) holds for any parse rejection.
	ErrParse = errors.New("hashline: parse error")
	// ErrMissingHeader is returned when a section has no ¶PATH#TAG header.
	ErrMissingHeader = errors.New("hashline: missing section header")
	// ErrMissingTag is returned when a header carries no #TAG.
	ErrMissingTag = errors.New("hashline: missing snapshot tag")
	// ErrInvalidTag is returned when a tag is not exactly four uppercase
	// hex digits.
	ErrInvalidTag = errors.New("hashline: invalid snapshot tag")
	// ErrInvalidOperation is returned for an unknown operation keyword or a
	// malformed line/range specifier.
	ErrInvalidOperation = errors.New("hashline: invalid operation")
	// ErrInvalidBodyRow is returned for a body row that does not start with
	// '+', or for a delete operation that carries a body.
	ErrInvalidBodyRow = errors.New("hashline: invalid body row")
	// ErrSnapshotMismatch is returned by the patcher when the section tag
	// does not match the live file's computed tag (stale-reject).
	ErrSnapshotMismatch = errors.New("hashline: snapshot tag does not match live file")
	// ErrNoop is returned when an applied patch leaves the file unchanged.
	ErrNoop = errors.New("hashline: edit is a no-op")
	// ErrDuplicateSection is returned by the patcher when a single patch
	// contains more than one section for the same canonical path. Each
	// section validates against (and writes) the live file independently, so
	// a second section would silently clobber the first; the agent must
	// instead combine the edits into one section with multiple operations.
	ErrDuplicateSection = errors.New("hashline: multiple sections target the same file")
	// ErrRecoveryConflict is returned by the patcher when a stale edit could
	// not be auto-recovered because the live file and the re-applied edit
	// changed the same lines in incompatible ways (the three-way merge
	// conflicts). It wraps ErrSnapshotMismatch so existing callers that match
	// errors.Is(err, ErrSnapshotMismatch) — and the agent-facing re-read
	// recovery message — still trigger, while callers that want to
	// distinguish a true conflict from a plain stale tag can match it
	// directly.
	ErrRecoveryConflict = fmt.Errorf("hashline: stale edit conflicts with concurrent changes: %w", ErrSnapshotMismatch)
)
