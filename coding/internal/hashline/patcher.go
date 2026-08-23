package hashline

import (
	"context"
	"errors"
	"fmt"
)

// Filesystem is the disk boundary for the patcher. The hashline package
// never touches the real filesystem directly; the host supplies a
// sandboxed implementation (coding.Workspace). All paths are
// workspace-relative as they appear in section headers.
type Filesystem interface {
	// CanonicalPath validates and canonicalizes a workspace-relative path,
	// rejecting escapes. The returned path is used in result headers.
	CanonicalPath(path string) (string, error)
	// ReadText reads the full file content (raw bytes as a string).
	ReadText(ctx context.Context, path string) (string, error)
	// PreflightWrite checks that a write to path can proceed (e.g. parent
	// dir writable, not denied) without performing it.
	PreflightWrite(ctx context.Context, path string) error
	// WriteText writes the full file content.
	WriteText(ctx context.Context, path, text string) error
}

// Patcher applies parsed hashline patches all-or-nothing against a
// Filesystem. Snapshots is optional and nil-safe: the stale-reject path
// reads live files and compares tags directly, so a nil store behaves like
// LazySnapshotStore.
type Patcher struct {
	FS        Filesystem
	Snapshots SnapshotStore
}

// PreparedSection is a section that passed preflight: its tag matched the
// live file and its ops applied cleanly in memory. The original and new
// text are retained so Commit can write and, on failure, roll back.
type PreparedSection struct {
	// Path is the canonical path from the Filesystem.
	Path string
	// OldTag is the tag the section referenced (and that matched live).
	OldTag string
	// NewTag is the tag of the applied result.
	NewTag string
	// Op summarizes the section's operations for the result header.
	Op string
	// originalRaw is the live file's raw bytes, kept for rollback.
	originalRaw string
	// normalized captures the BOM/line-ending shape for restore on write.
	normalized NormalizedFile
	// newText is the LF-internal applied content.
	newText string
	// FirstChangedLine is the first changed 1-based line.
	FirstChangedLine int
	// Diff is the compact diff between old and new content.
	Diff string
	// Warnings carries non-fatal advisories from Apply.
	Warnings []string
	// Recovered reports that a stale section was safely replayed against current
	// content after anchor verification/remapping when required (spec §4.7).
	Recovered bool
}

// SectionResult is the per-section outcome surfaced to the caller after a
// successful commit.
type SectionResult struct {
	Path             string
	OldTag           string
	NewTag           string
	Op               string
	Header           string
	FirstChangedLine int
	Diff             string
	Warnings         []string
	// Recovered reports that a stale section was safely replayed against current
	// content after anchor verification/remapping when required (spec §4.7).
	Recovered bool
}

// ApplyPatchResult is the typed result of applying a multi-section patch.
type ApplyPatchResult struct {
	Sections []SectionResult
}

// store returns the configured store or a lazy no-op store.
func (p *Patcher) store() SnapshotStore {
	if p.Snapshots == nil {
		return LazySnapshotStore{}
	}
	return p.Snapshots
}

// Preflight validates every section against the live filesystem and
// applies each in memory, returning prepared sections without writing. It
// is the read-only half of Apply and backs the edit_hashline dry_run mode.
//
// Errors: a path/canonicalization failure, ErrSnapshotMismatch on a stale
// tag, or an ApplyError/ErrNoop on a section that cannot apply, abort the
// whole preflight (nothing is written and Commit is never reached).
func (p *Patcher) Preflight(ctx context.Context, patch Patch) ([]PreparedSection, error) {
	prepared := make([]PreparedSection, 0, len(patch.Sections))
	// seen guards against two sections targeting the same underlying file.
	// Each section is validated and written against the live file
	// independently, so a second section for the same file would clobber the
	// first's edits during Commit; reject the patch and tell the agent to
	// fold the edits into a single section (a section may carry many ops). The
	// key is the file's resolved on-disk identity when the Filesystem can
	// supply one (identityResolver), so two sections that alias the same file
	// through different in-root symlinks (e.g. "a.go" and "link.go" -> "a.go")
	// collapse to one key; otherwise it falls back to the canonical path.
	seen := make(map[string]int, len(patch.Sections))
	for _, sec := range patch.Sections {
		canon, err := p.FS.CanonicalPath(sec.Path)
		if err != nil {
			return nil, fmt.Errorf("hashline: section %q: %w", sec.Path, err)
		}

		key := canon
		if ir, ok := p.FS.(identityResolver); ok {
			if id, idErr := ir.ResolveIdentity(canon); idErr == nil {
				key = id
			}
		}
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("hashline: section %q (also at section %d): %w; combine the operations into one ¶PATH#TAG section",
				canon, first+1, ErrDuplicateSection)
		}
		seen[key] = len(prepared)

		raw, err := p.FS.ReadText(ctx, canon)
		if err != nil {
			return nil, fmt.Errorf("hashline: read %q: %w", canon, err)
		}

		nf := Normalize(raw)
		liveTag := ComputeFileHash(nf.Text)

		var (
			newText   string
			fcl       int
			warnings  []string
			recovered bool
		)
		if liveTag == sec.Tag {
			// Guard the 16-bit tag against a fingerprint collision before trusting
			// the fast path. sec.Tag is only the low 16 bits of FNV, so it is not a
			// unique handle: a different out-of-band version can share it, and two
			// distinct versions read in one session can collide on it. Every
			// read_file/search/edit records the content its tag was minted from
			// (§4.8) and the store retains colliding versions distinctly, so when any
			// history is retained for this path under the tag (ByHash ok) take the
			// fast path only against an UNAMBIGUOUS base: the tag must pin to a single
			// recorded content (UniqueByHash) that equals the live file. If the tag is
			// ambiguous (two distinct recorded versions collide on it) we cannot tell
			// which one the edit's line numbers target, and if the single recorded
			// content differs from live the file changed out of band — either way
			// reject as stale and force a re-read. With no retained history (lazy
			// store, or evicted) the tag is the only available check, as before.
			st := p.store()
			if _, known := st.ByHash(canon, sec.Tag); known {
				base, unique := st.UniqueByHash(canon, sec.Tag)
				if !unique || base.Text != nf.Text {
					return nil, fmt.Errorf("hashline: section %q: %w (tag %s does not uniquely identify the live content — the file changed out of band, or its 16-bit tag collides with another version read this session, so the edit's line numbers cannot be trusted; re-read it before editing)",
						canon, ErrSnapshotMismatch, sec.Tag)
				}
			}
			// Fast path: the tag matches the live file, so it IS the version the
			// edit was built on. Resolve any go/ast block ops (replace/delete
			// block N) to concrete line-range ops against it, then apply; resolved
			// block ops compose with the applier's overlap/conflict detection
			// exactly like hand-written range ops. Block resolution is deferred to
			// this branch (rather than run against the live file unconditionally)
			// so a STALE block edit can still recover against its recorded base in
			// recoverStale, instead of aborting here when the live file no longer
			// parses or the block has moved off the referenced line.
			resolvedSec, err := resolveBlockOps(nf.Text, sec)
			if err != nil {
				return nil, fmt.Errorf("hashline: %w", err)
			}
			applied, err := Apply(nf.Text, resolvedSec)
			if err != nil {
				return nil, fmt.Errorf("hashline: apply %q: %w", canon, err)
			}
			newText = applied.Text
			fcl = applied.FirstChangedLine
			warnings = applied.Warnings
		} else {
			// Stale tag. Attempt verified anchor remapping against recorded
			// history; missing bases and unprovable mappings preserve the strict
			// ErrSnapshotMismatch re-read path.
			rec, err := p.recoverStale(canon, sec, nf.Text, liveTag)
			if err != nil {
				return nil, err
			}
			newText = rec.text
			fcl = rec.firstChangedLine
			warnings = rec.warnings
			recovered = true
		}

		newTag := ComputeFileHash(newText)
		prepared = append(prepared, PreparedSection{
			Path:             canon,
			OldTag:           sec.Tag,
			NewTag:           newTag,
			Op:               summarizeOps(sec.Ops),
			originalRaw:      raw,
			normalized:       nf,
			newText:          newText,
			FirstChangedLine: fcl,
			Diff:             CompactDiff(nf.Text, newText),
			Warnings:         warnings,
			Recovered:        recovered,
		})
	}
	return prepared, nil
}

// recovered is the outcome of a successful stale-edit recovery.
type recovered struct {
	text             string
	firstChangedLine int
	warnings         []string
}

// recoverStale attempts to salvage a stale-tag edit by proving that every
// targeted line still exists unchanged in the live file, remapping the old line
// anchors to their live positions, and replaying the operation directly on the
// current content. This preserves unrelated concurrent changes without asking a
// general-purpose merge to infer the model's target.
//
// Block operations are resolved against the recorded base first because their
// line numbers describe that version. Head/tail-only edits have no line anchors
// and are safe to replay directly. Missing history, changed target lines,
// inconsistent offsets, ambiguous context, and excessive drift fail closed with
// ErrSnapshotMismatch so the agent re-reads.
func (p *Patcher) recoverStale(canon string, sec Section, liveText, liveTag string) (recovered, error) {
	base, ok := p.store().UniqueByHash(canon, sec.Tag)
	if !ok {
		return recovered{}, fmt.Errorf("hashline: section %q: %w (live tag %s, edit assumed %s; no unique recorded base for that tag, re-read the file before editing)",
			canon, ErrSnapshotMismatch, liveTag, sec.Tag)
	}

	resolvedSec, err := resolveBlockOps(base.Text, sec)
	if err != nil {
		return recovered{}, fmt.Errorf("hashline: section %q: %w (live tag %s, edit assumed %s; the block edit no longer resolves against the recorded version, re-read the file before editing)",
			canon, ErrSnapshotMismatch, liveTag, sec.Tag)
	}

	// Validate the operation against the version the model actually read. The
	// desired text is used only for conservative already-applied detection when
	// anchor remapping fails; recovered writes always replay the original
	// operation against live content.
	desired, err := Apply(base.Text, resolvedSec)
	if err != nil {
		return recovered{}, fmt.Errorf("hashline: section %q: %w (live tag %s, edit assumed %s; the edit no longer applies to the recorded version, re-read the file before editing)",
			canon, ErrSnapshotMismatch, liveTag, sec.Tag)
	}

	remapped, ok := remapSectionToLive(base.Text, liveText, resolvedSec)
	if !ok {
		// Preserve the no-op contract only when the exact requested result is
		// already current. General merge work here would defeat the bounded
		// recovery budget and is never allowed to authorize a write.
		if desired.Text == liveText {
			return recovered{}, fmt.Errorf("hashline: section %q: %w", canon, ErrNoop)
		}
		return recovered{}, fmt.Errorf("hashline: section %q: %w (live tag %s, edit assumed %s; target anchors changed, moved inconsistently, or could not be mapped unambiguously, re-read the file before editing)",
			canon, ErrRecoveryConflict, liveTag, sec.Tag)
	}

	applied, err := Apply(liveText, remapped.section)
	if err != nil {
		if errors.Is(err, ErrNoop) {
			return recovered{}, fmt.Errorf("hashline: section %q: %w", canon, ErrNoop)
		}
		return recovered{}, fmt.Errorf("hashline: section %q: %w (live tag %s, edit assumed %s; remapped operations could not be applied safely, re-read the file before editing)",
			canon, ErrRecoveryConflict, liveTag, sec.Tag)
	}

	warning := "recovered stale edit via verified anchor replay (target lines remained unchanged in the live file)"
	if !remapped.anchored {
		warning = "recovered stale head/tail edit against current content"
	} else if remapped.offset != 0 {
		warning = fmt.Sprintf("recovered stale edit via verified anchor remap (%+d lines)", remapped.offset)
	}
	warnings := make([]string, 0, len(applied.Warnings)+1)
	warnings = append(warnings, warning)
	warnings = append(warnings, applied.Warnings...)

	return recovered{
		text:             applied.Text,
		firstChangedLine: applied.FirstChangedLine,
		warnings:         warnings,
	}, nil
}

// Commit writes every prepared section, restoring its original BOM and
// line ending. It is all-or-nothing: it preflights all writes first, then
// writes; if any write fails, already-written files are restored from the
// rollback buffer before the error is returned.
func (p *Patcher) Commit(ctx context.Context, prepared []PreparedSection) (ApplyPatchResult, error) {
	// Preflight every write before mutating anything.
	for _, ps := range prepared {
		if err := p.FS.PreflightWrite(ctx, ps.Path); err != nil {
			return ApplyPatchResult{}, fmt.Errorf("hashline: preflight write %q: %w", ps.Path, err)
		}
	}

	written := make([]PreparedSection, 0, len(prepared))
	for _, ps := range prepared {
		out := ps.normalized.Restore(ps.newText)
		if err := p.FS.WriteText(ctx, ps.Path, out); err != nil {
			if rbErr := p.rollback(ctx, written); rbErr != nil {
				return ApplyPatchResult{}, fmt.Errorf("hashline: write %q: %w", ps.Path, errors.Join(err, rbErr))
			}
			return ApplyPatchResult{}, fmt.Errorf("hashline: write %q: %w", ps.Path, err)
		}
		written = append(written, ps)
	}

	store := p.store()
	results := make([]SectionResult, 0, len(prepared))
	for _, ps := range written {
		// Record the new content so a future history-backed store can serve
		// ByHash; the lazy store ignores this.
		store.Record(ps.Path, ps.newText)
		results = append(results, SectionResult{
			Path:             ps.Path,
			OldTag:           ps.OldTag,
			NewTag:           ps.NewTag,
			Op:               ps.Op,
			Header:           FormatHeader(ps.Path, ps.NewTag),
			FirstChangedLine: ps.FirstChangedLine,
			Diff:             ps.Diff,
			Warnings:         ps.Warnings,
			Recovered:        ps.Recovered,
		})
	}
	return ApplyPatchResult{Sections: results}, nil
}

// identityResolver is an optional Filesystem capability used by Preflight's
// duplicate-section guard to key on a file's resolved on-disk identity rather
// than its lexical canonical path. Two sections can name the same underlying
// file through different in-root symlink aliases (e.g. "a.go" and a "link.go"
// that points at it); keyed lexically they slip past the guard and the second
// Commit write clobbers the first section's edit. When the Filesystem
// implements identityResolver, the guard dedups on the resolved identity so
// aliases collapse to one key; a Filesystem that does not implement it falls
// back to the canonical path (no alias detection). ResolveIdentity receives the
// already-canonicalized path.
type identityResolver interface {
	ResolveIdentity(path string) (string, error)
}

// restorer is an optional Filesystem capability used only by rollback. Its
// RestoreText writes bytes that were previously READ from the same file,
// bypassing any forward write-size cap WriteText enforces. The original bytes
// were already on disk (the read succeeded under the larger read cap), so
// restoring them must always be allowed: otherwise a multi-section edit that
// first shrinks an over-write-cap file and then fails on a later section could
// not be rolled back through the capped WriteText, leaving the earlier file
// modified and breaking Commit's all-or-nothing contract. A Filesystem that
// does not implement restorer falls back to WriteText (best-effort, as before).
type restorer interface {
	RestoreText(ctx context.Context, path, text string) error
}

// rollback restores already-written files to their original raw bytes. Any
// restore error is best-effort; the original write failure is the reported
// cause. Restoration prefers the uncapped restorer path so an original that
// exceeds the forward write cap is still put back (see restorer).
//
// The rollback detaches cancellation and deadlines from ctx: a canceled or
// timed-out edit context is itself a common cause of the write failure that
// triggers rollback, and the workspace's WriteText/RestoreText short-circuit on
// ctx.Err(). Reusing the canceled context would skip every restore and leave
// the earlier sections modified, breaking Commit's all-or-nothing contract. The
// restore writes are synchronous and bounded, so dropping cancellation here
// cannot hang the rollback.
func (p *Patcher) rollback(ctx context.Context, written []PreparedSection) error {
	ctx = context.WithoutCancel(ctx)
	var errs []error
	for i := len(written) - 1; i >= 0; i-- {
		var err error
		if r, ok := p.FS.(restorer); ok {
			err = r.RestoreText(ctx, written[i].Path, written[i].originalRaw)
		} else {
			err = p.FS.WriteText(ctx, written[i].Path, written[i].originalRaw)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %q: %w", written[i].Path, err))
		}
	}
	return errors.Join(errs...)
}

// Apply runs the full parse-validated → live-hash compare → in-memory
// apply → all-or-nothing write sequence for an already-parsed patch.
func (p *Patcher) Apply(ctx context.Context, patch Patch) (ApplyPatchResult, error) {
	prepared, err := p.Preflight(ctx, patch)
	if err != nil {
		return ApplyPatchResult{}, err
	}
	return p.Commit(ctx, prepared)
}

// summarizeOps renders a section's operations as a short, human-readable
// summary for the result header (e.g. "replace 3..5", or
// "replace 1..2, insert tail").
func summarizeOps(ops []Op) string {
	if len(ops) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.Kind {
		case OpReplace, OpDelete:
			if op.Start == op.End {
				parts = append(parts, fmt.Sprintf("%s %d", op.Kind, op.Start))
			} else {
				parts = append(parts, fmt.Sprintf("%s %d..%d", op.Kind, op.Start, op.End))
			}
		case OpInsertBefore:
			parts = append(parts, fmt.Sprintf("insert before %d", op.Start))
		case OpInsertAfter:
			parts = append(parts, fmt.Sprintf("insert after %d", op.Start))
		case OpInsertHead:
			parts = append(parts, "insert head")
		case OpInsertTail:
			parts = append(parts, "insert tail")
		case OpReplaceBlock:
			parts = append(parts, fmt.Sprintf("replace block %d", op.Start))
		case OpDeleteBlock:
			parts = append(parts, fmt.Sprintf("delete block %d", op.Start))
		}
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
