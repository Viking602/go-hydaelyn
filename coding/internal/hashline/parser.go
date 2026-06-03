package hashline

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseError is a precise, line-numbered parse failure. Line is the
// 1-based line number within the patch input. Err is one of the sentinel
// errors (which all wrap ErrParse), so callers can match either the
// specific cause or ErrParse.
type ParseError struct {
	Line int
	Msg  string
	Err  error
}

// Error renders the parse error with its input line number.
func (e *ParseError) Error() string {
	return fmt.Sprintf("hashline: line %d: %s", e.Line, e.Msg)
}

// Unwrap exposes the wrapped sentinel for errors.Is.
func (e *ParseError) Unwrap() error { return e.Err }

// newParseError builds a ParseError. The given sentinel is wrapped so that
// errors.Is(result, ErrParse) and errors.Is(result, sentinel) both hold.
func newParseError(line int, sentinel error, format string, args ...any) *ParseError {
	return &ParseError{
		Line: line,
		Msg:  fmt.Sprintf(format, args...),
		Err:  wrapParse(sentinel),
	}
}

// parseWrap pairs a sentinel with ErrParse so a single errors.Is can match
// either. It is realized via a small joined-error value.
func wrapParse(sentinel error) error {
	if sentinel == ErrParse {
		return ErrParse
	}
	return parseCause{sentinel: sentinel}
}

// parseCause makes both the specific sentinel and ErrParse match under
// errors.Is, without disturbing the sentinel's own identity.
type parseCause struct{ sentinel error }

func (c parseCause) Error() string { return c.sentinel.Error() }

// Is reports a match for the wrapped sentinel or for ErrParse.
func (c parseCause) Is(target error) bool {
	return target == c.sentinel || target == ErrParse
}

func (c parseCause) Unwrap() error { return c.sentinel }

// Parse parses a hashline patch input into a Patch. The grammar is strict
// (see docs/coding-agent-hashline.md section 3.2 and 4.5): every section
// begins with a ¶PATH#TAG header; body rows are final content only and
// must start with '+'; delete carries no body; no -old rows, no bare
// context rows, no unknown operations. Blank lines separate operations and
// are otherwise ignored.
func Parse(input string) (Patch, error) {
	// Operate on the LF-normalized form so CRLF inputs parse identically;
	// a leading BOM on the patch itself is stripped too.
	nf := Normalize(input)
	lines := strings.Split(nf.Text, lf)

	p := &parseState{cur: -1}
	for i, line := range lines {
		if err := p.consume(i+1, line); err != nil {
			return Patch{}, err
		}
	}
	if err := p.flush(); err != nil {
		return Patch{}, err
	}

	if len(p.patch.Sections) == 0 {
		return Patch{}, newParseError(1, ErrMissingHeader,
			"input contains no ¶PATH#TAG section")
	}
	return p.patch, nil
}

// parseState carries the in-progress patch while Parse walks the input one
// line at a time. cur is the index of the section currently being built (or
// -1 when none is open); pending is an operation still collecting body rows
// (or nil); pendingLine is the input line where that op's header appeared.
type parseState struct {
	patch       Patch
	cur         int
	pending     *Op
	pendingLine int
}

// consume classifies and applies a single input line (1-based lineNo).
func (p *parseState) consume(lineNo int, line string) error {
	switch {
	case strings.HasPrefix(line, sectionPrefix):
		return p.openSection(lineNo, line)
	case strings.HasPrefix(line, "+"):
		return p.appendBody(lineNo, line)
	case strings.HasPrefix(line, "-"):
		// Old/context-removal rows are not part of the grammar.
		return newParseError(lineNo, ErrInvalidBodyRow,
			"-old rows are not allowed; body rows are final content only (+TEXT)")
	case strings.TrimSpace(line) == "":
		// Blank lines terminate an op's body but are otherwise ignored.
		return p.flush()
	default:
		return p.openOp(lineNo, line)
	}
}

// flush finalizes the pending op (if any) onto the current section,
// validating body-row requirements that can only be checked once the body is
// complete.
func (p *parseState) flush() error {
	if p.pending == nil {
		return nil
	}
	switch p.pending.Kind {
	case OpReplace, OpReplaceBlock, OpInsertBefore, OpInsertAfter, OpInsertHead, OpInsertTail:
		if len(p.pending.Body) == 0 {
			return newParseError(p.pendingLine, ErrInvalidBodyRow,
				"operation %q requires at least one +body row", p.pending.Kind)
		}
	case OpDelete, OpDeleteBlock:
		if len(p.pending.Body) != 0 {
			return newParseError(p.pendingLine, ErrInvalidBodyRow,
				"delete operation must not carry a body")
		}
	}
	p.patch.Sections[p.cur].Ops = append(p.patch.Sections[p.cur].Ops, *p.pending)
	p.pending = nil
	return nil
}

// openSection closes any pending op and starts a new ¶PATH#TAG section.
func (p *parseState) openSection(lineNo int, line string) error {
	if err := p.flush(); err != nil {
		return err
	}
	sec, err := parseHeader(lineNo, line)
	if err != nil {
		return err
	}
	p.patch.Sections = append(p.patch.Sections, sec)
	p.cur = len(p.patch.Sections) - 1
	return nil
}

// appendBody validates and records a +body row on the pending op.
func (p *parseState) appendBody(lineNo int, line string) error {
	// A body row only makes sense while an op is collecting one.
	if p.pending == nil {
		return newParseError(lineNo, ErrInvalidBodyRow,
			"body row without a preceding operation")
	}
	if p.pending.Kind == OpDelete || p.pending.Kind == OpDeleteBlock {
		return newParseError(lineNo, ErrInvalidBodyRow,
			"delete operation must not carry a body")
	}
	p.pending.Body = append(p.pending.Body, line[1:])
	return nil
}

// openOp closes any pending op and parses a new operation header for the open
// section.
func (p *parseState) openOp(lineNo int, line string) error {
	// Anything here must be an operation header for the open section. If no
	// section is open, the input is missing a header.
	if p.cur < 0 {
		return newParseError(lineNo, ErrMissingHeader,
			"expected a ¶PATH#TAG section header before any operation")
	}
	// A body-taking op that is still collecting rows cannot be followed by a
	// non-+ line: that is a bare context line, not a new operation. Report it
	// precisely at the stray line.
	bodyPending := p.pending != nil && takesBody(p.pending.Kind)
	if bodyPending && len(p.pending.Body) == 0 {
		return newParseError(lineNo, ErrInvalidOperation,
			"bare context line %q; body rows must start with '+'", line)
	}
	if err := p.flush(); err != nil {
		return err
	}
	op, err := parseOpHeader(lineNo, line)
	if err != nil {
		// We were inside a body-taking op (with at least one +row) and this
		// line is neither a +row nor a recognized operation header — it is a
		// stray context line. Tell the agent to prefix it with '+' rather than
		// misreporting it as an unknown operation. A line that *does* start
		// with an op keyword (but is otherwise malformed) keeps its specific
		// error so genuine header mistakes are not masked.
		if bodyPending && !looksLikeOpHeader(line) {
			return newParseError(lineNo, ErrInvalidOperation,
				"bare context line %q; body rows must start with '+'", line)
		}
		return err
	}
	p.pending = &op
	p.pendingLine = lineNo
	return nil
}

// looksLikeOpHeader reports whether line begins with a recognized operation
// keyword (replace/delete/insert). It does not validate the operation's
// arguments — it only distinguishes an intended (possibly malformed) header
// from a stray context line.
func looksLikeOpHeader(line string) bool {
	fields := strings.Fields(strings.TrimSuffix(line, ":"))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "replace", "delete", "insert":
		return true
	default:
		return false
	}
}

// takesBody reports whether an operation expects at least one +body row.
func takesBody(k OpKind) bool {
	switch k {
	case OpReplace, OpReplaceBlock, OpInsertBefore, OpInsertAfter, OpInsertHead, OpInsertTail:
		return true
	default:
		return false
	}
}

// parseHeader parses a ¶PATH#TAG header line into a Section (with no ops).
func parseHeader(lineNo int, line string) (Section, error) {
	rest := strings.TrimPrefix(line, sectionPrefix)
	hash := strings.LastIndexByte(rest, '#')
	if hash < 0 {
		return Section{}, newParseError(lineNo, ErrMissingTag,
			"header %q has no #TAG", line)
	}
	path := rest[:hash]
	tag := rest[hash+1:]
	if path == "" {
		return Section{}, newParseError(lineNo, ErrMissingHeader,
			"header has an empty path")
	}
	if tag == "" {
		return Section{}, newParseError(lineNo, ErrMissingTag,
			"header %q has an empty tag", line)
	}
	if !isValidTag(tag) {
		return Section{}, newParseError(lineNo, ErrInvalidTag,
			"tag %q is not four uppercase hex digits", tag)
	}
	return Section{Path: path, Tag: tag}, nil
}

// isValidTag reports whether tag is exactly four uppercase hex digits.
func isValidTag(tag string) bool {
	if len(tag) != 4 {
		return false
	}
	for _, c := range tag {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// parseOpHeader parses an operation header line (e.g. "replace 3..5:",
// "delete 2..4", "insert before 7:", "insert head:") into an Op with no
// body yet.
func parseOpHeader(lineNo int, line string) (Op, error) {
	// Strip a single trailing colon if present; operations that take a
	// body use it, delete does not, and we tolerate either spelling so the
	// body-presence rules in flush() are the single source of truth.
	spec := strings.TrimSuffix(line, ":")
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return Op{}, newParseError(lineNo, ErrInvalidOperation, "empty operation")
	}

	switch fields[0] {
	case "replace":
		// "replace block N" targets a Go syntactic block; "replace N..M" a
		// line range. The block extent is resolved later (ResolveBlocks).
		if len(fields) >= 2 && fields[1] == "block" {
			n, err := parseBlockLine(lineNo, fields[2:])
			if err != nil {
				return Op{}, err
			}
			return Op{Kind: OpReplaceBlock, Start: n, End: n}, nil
		}
		start, end, err := parseRange(lineNo, fields[1:])
		if err != nil {
			return Op{}, err
		}
		return Op{Kind: OpReplace, Start: start, End: end}, nil

	case "delete":
		if len(fields) >= 2 && fields[1] == "block" {
			n, err := parseBlockLine(lineNo, fields[2:])
			if err != nil {
				return Op{}, err
			}
			return Op{Kind: OpDeleteBlock, Start: n, End: n}, nil
		}
		start, end, err := parseRange(lineNo, fields[1:])
		if err != nil {
			return Op{}, err
		}
		return Op{Kind: OpDelete, Start: start, End: end}, nil

	case "insert":
		return parseInsert(lineNo, fields[1:], line)

	default:
		return Op{}, newParseError(lineNo, ErrInvalidOperation,
			"unknown operation %q", fields[0])
	}
}

// parseRange parses the "N" or "N..M" argument of replace/delete. A bare N
// is treated as the single-line range N..N.
func parseRange(lineNo int, args []string) (int, int, error) {
	if len(args) != 1 {
		return 0, 0, newParseError(lineNo, ErrInvalidOperation,
			"expected a single N or N..M range argument, got %d fields", len(args))
	}
	arg := args[0]
	if i := strings.Index(arg, ".."); i >= 0 {
		startStr := arg[:i]
		endStr := arg[i+2:]
		start, err := parseLineNumber(lineNo, startStr)
		if err != nil {
			return 0, 0, err
		}
		end, err := parseLineNumber(lineNo, endStr)
		if err != nil {
			return 0, 0, err
		}
		if start > end {
			return 0, 0, newParseError(lineNo, ErrInvalidOperation,
				"range start %d is greater than end %d", start, end)
		}
		return start, end, nil
	}
	n, err := parseLineNumber(lineNo, arg)
	if err != nil {
		return 0, 0, err
	}
	return n, n, nil
}

// parseBlockLine parses the single line-number argument of a block op
// ("replace block N" / "delete block N"). Unlike parseRange, a block op
// targets exactly one start line and never takes an N..M range, so a range
// argument is rejected.
func parseBlockLine(lineNo int, args []string) (int, error) {
	if len(args) != 1 {
		return 0, newParseError(lineNo, ErrInvalidOperation,
			"block operation requires a single start line number, got %d fields", len(args))
	}
	if strings.Contains(args[0], "..") {
		return 0, newParseError(lineNo, ErrInvalidOperation,
			"block operation takes a single start line, not a range %q", args[0])
	}
	return parseLineNumber(lineNo, args[0])
}

// parseInsert parses the variants of the insert operation:
// "insert before N", "insert after N", "insert head", "insert tail".
func parseInsert(lineNo int, args []string, original string) (Op, error) {
	if len(args) == 0 {
		return Op{}, newParseError(lineNo, ErrInvalidOperation,
			"insert requires a target (before N, after N, head, tail)")
	}
	switch args[0] {
	case "before", "after":
		if len(args) != 2 {
			return Op{}, newParseError(lineNo, ErrInvalidOperation,
				"insert %s requires a single line number", args[0])
		}
		n, err := parseLineNumber(lineNo, args[1])
		if err != nil {
			return Op{}, err
		}
		kind := OpInsertBefore
		if args[0] == "after" {
			kind = OpInsertAfter
		}
		return Op{Kind: kind, Start: n, End: n}, nil
	case "head":
		if len(args) != 1 {
			return Op{}, newParseError(lineNo, ErrInvalidOperation,
				"insert head takes no arguments")
		}
		return Op{Kind: OpInsertHead}, nil
	case "tail":
		if len(args) != 1 {
			return Op{}, newParseError(lineNo, ErrInvalidOperation,
				"insert tail takes no arguments")
		}
		return Op{Kind: OpInsertTail}, nil
	default:
		return Op{}, newParseError(lineNo, ErrInvalidOperation,
			"unknown insert target %q in %q", args[0], original)
	}
}

// parseLineNumber parses a positive 1-based line number, rejecting
// non-numeric and < 1 values.
func parseLineNumber(lineNo int, s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, newParseError(lineNo, ErrInvalidOperation,
			"line number %q is not an integer", s)
	}
	if n < 1 {
		return 0, newParseError(lineNo, ErrInvalidOperation,
			"line number %d must be >= 1", n)
	}
	return n, nil
}
