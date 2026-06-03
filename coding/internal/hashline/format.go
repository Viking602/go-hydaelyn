package hashline

import (
	"strconv"
	"strings"
)

// sectionPrefix marks the start of a hashline section header.
const sectionPrefix = "¶"

// FormatHeader renders a section header as ¶PATH#TAG.
func FormatHeader(path, tag string) string {
	return sectionPrefix + path + "#" + tag
}

// FormatNumberedLine renders a single numbered line as N:TEXT, where N is
// the 1-based line number and TEXT is the verbatim line content.
func FormatNumberedLine(n int, text string) string {
	return strconv.Itoa(n) + ":" + text
}

// CompactDiff renders a minimal line diff between two LF-internal texts in
// the section-3.3 output style: a contiguous block where '-' rows are
// original lines that were removed and '+' rows are new lines that were
// added. It uses a longest-common-prefix/suffix trim around the changed
// span, which is sufficient for the bounded, localized edits hashline
// produces (it is not a full Myers diff and is not meant to be).
func CompactDiff(oldText, newText string) string {
	o := splitLines(oldText)
	n := splitLines(newText)

	// Trim the common prefix.
	pre := 0
	for pre < len(o) && pre < len(n) && o[pre] == n[pre] {
		pre++
	}
	// Trim the common suffix, not crossing into the prefix.
	suf := 0
	for suf < len(o)-pre && suf < len(n)-pre && o[len(o)-1-suf] == n[len(n)-1-suf] {
		suf++
	}

	var b strings.Builder
	for i := pre; i < len(o)-suf; i++ {
		b.WriteString("-")
		b.WriteString(o[i])
		b.WriteString(lf)
	}
	for i := pre; i < len(n)-suf; i++ {
		b.WriteString("+")
		b.WriteString(n[i])
		b.WriteString(lf)
	}
	return strings.TrimSuffix(b.String(), lf)
}

// FormatNumberedLines renders LF-internal text as joined "N:TEXT" rows,
// the first row numbered startLine. The lines are joined with LF and no
// trailing newline is appended.
//
// A trailing newline in text denotes a final empty line, which is
// rendered as its own numbered row (matching the read_file output format).
func FormatNumberedLines(text string, startLine int) string {
	if text == "" {
		return FormatNumberedLine(startLine, "")
	}
	lines := strings.Split(text, lf)
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = FormatNumberedLine(startLine+i, line)
	}
	return strings.Join(rows, lf)
}
