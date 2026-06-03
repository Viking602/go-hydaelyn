package hashline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// ErrBlockResolve is returned when a block operation cannot be resolved to a
// concrete line range: the file is not valid Go, or no Go block begins on the
// requested line. It wraps ErrParse so callers that match ErrParse still
// trigger, while the message tells the agent to fall back to a line-range op.
//
// Block edit is Go-only. It uses go/parser + go/ast over the standard library
// (no tree-sitter, no cgo). A non-Go file, or syntactically invalid Go, always
// fails resolution; the agent should use replace/delete with an explicit line
// range instead.
var ErrBlockResolve = fmt.Errorf("hashline: cannot resolve Go block: %w", ErrParse)

// blockRange is the inclusive 1-based line span of a resolved Go block,
// carrying the kind priority used to break ties when several nodes begin on
// the same line.
type blockRange struct {
	start int
	end   int
	// priority orders candidates that share the matched start line: a
	// top-level declaration outranks its own body block and any statement that
	// happens to open on the declaration line, so "replace block N" on a func
	// line selects the whole function (with its doc comment) rather than just
	// the body. Higher wins.
	priority int
}

// Candidate priorities (higher selected first on a same-line tie).
const (
	blockPriStmt = 0 // ordinary statements and block statements
	blockPriDecl = 2 // top-level declarations (func/gen)
)

// ResolveBlock parses text as a Go source file and returns the inclusive
// 1-based [startLine, endLine] range of the smallest top-level-or-statement
// syntactic node whose start line is line. Candidate nodes are top-level
// declarations (func/gen decls), block statements, and statements; among the
// nodes that begin on line, the one with the smallest line span is chosen.
//
// A node carrying a leading doc comment matches either on its keyword line or
// on the doc comment's first line, and the returned range always begins at the
// doc comment so a replacement (or deletion) covers the comment too.
//
// If text is not valid Go, or no block begins on line, ResolveBlock returns an
// error wrapping ErrBlockResolve. Block edit is Go-only; see ErrBlockResolve.
func ResolveBlock(text string, line int) (int, int, error) {
	if line < 1 {
		return 0, 0, fmt.Errorf("%w: line %d must be >= 1", ErrBlockResolve, line)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", text, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: file is not valid Go (%v); use a line-range replace/delete instead", ErrBlockResolve, err)
	}

	candidates := collectBlocks(fset, file, line)
	if len(candidates) == 0 {
		return 0, 0, fmt.Errorf("%w: no Go block starts on line %d; use a line-range replace/delete instead", ErrBlockResolve, line)
	}

	// A top-level declaration always wins over a contained block or statement
	// that opens on the same matched line (so "replace block N" on a func line
	// takes the whole function, doc comment included). Otherwise the smallest
	// span wins, ties broken toward the later (more deeply nested) start, so an
	// inner block is preferred over an enclosing one that shares a start line.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		si := candidates[i].end - candidates[i].start
		sj := candidates[j].end - candidates[j].start
		if si != sj {
			return si < sj
		}
		return candidates[i].start > candidates[j].start
	})

	best := candidates[0]
	return best.start, best.end, nil
}

// collectBlocks walks the AST and returns every candidate block whose start
// line (keyword line, or doc-comment first line when present) equals line.
func collectBlocks(fset *token.FileSet, file *ast.File, line int) []blockRange {
	var out []blockRange

	add := func(node ast.Node, doc *ast.CommentGroup, priority int) {
		if node == nil {
			return
		}
		keywordStart := fset.Position(node.Pos()).Line
		effectiveStart := keywordStart
		if doc != nil {
			if dl := fset.Position(doc.Pos()).Line; dl < effectiveStart {
				effectiveStart = dl
			}
		}
		endLine := fset.Position(node.End()).Line
		// Match if the agent pointed at the keyword line or the doc-comment
		// first line. The resolved range always begins at the effective start.
		if keywordStart == line || effectiveStart == line {
			out = append(out, blockRange{start: effectiveStart, end: endLine, priority: priority})
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			add(node, node.Doc, blockPriDecl)
		case *ast.GenDecl:
			add(node, node.Doc, blockPriDecl)
		case *ast.BlockStmt:
			add(node, nil, blockPriStmt)
		case ast.Stmt:
			// Any statement (if/for/switch/assign/expr/return/...) is a
			// candidate block. Excludes the *ast.BlockStmt case above, which
			// is handled with its own arm.
			add(node, nil, blockPriStmt)
		}
		return true
	})

	return out
}

// resolveBlockOps converts a section's block ops (OpReplaceBlock /
// OpDeleteBlock) into concrete line-range ops (OpReplace / OpDelete) against
// the given LF-internal Go source text, leaving non-block ops untouched. The
// resolved ops carry the block's [start,end] line range so the existing
// line-based applier (Apply) handles them unchanged, and they compose with the
// applier's overlap/conflict detection exactly like hand-written range ops.
//
// Resolution happens once, against the original file the section's tag refers
// to, before Apply runs, so an earlier op never shifts a later block's lines.
// Any unresolvable block aborts the whole section with an ErrBlockResolve
// error (block edit is Go-only).
func resolveBlockOps(text string, sec Section) (Section, error) {
	hasBlock := false
	for _, op := range sec.Ops {
		if op.Kind == OpReplaceBlock || op.Kind == OpDeleteBlock {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		return sec, nil
	}

	resolved := make([]Op, len(sec.Ops))
	for i, op := range sec.Ops {
		switch op.Kind {
		case OpReplaceBlock:
			start, end, err := ResolveBlock(text, op.Start)
			if err != nil {
				return Section{}, fmt.Errorf("section %q: replace block %d: %w", sec.Path, op.Start, err)
			}
			resolved[i] = Op{Kind: OpReplace, Start: start, End: end, Body: op.Body}
		case OpDeleteBlock:
			start, end, err := ResolveBlock(text, op.Start)
			if err != nil {
				return Section{}, fmt.Errorf("section %q: delete block %d: %w", sec.Path, op.Start, err)
			}
			resolved[i] = Op{Kind: OpDelete, Start: start, End: end}
		default:
			resolved[i] = op
		}
	}
	return Section{Path: sec.Path, Tag: sec.Tag, Ops: resolved}, nil
}
