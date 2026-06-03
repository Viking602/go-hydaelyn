package hashline

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// NormalizeForHash trims trailing space, tab, and carriage-return bytes
// from every line before the newline (or EOF) so that trailing-whitespace
// churn does not change a file's tag. The line structure (the count and
// position of "\n" separators) is preserved exactly; only the run of
// [ \t\r] immediately before each separator (and before EOF) is removed.
func NormalizeForHash(text string) string {
	lines := strings.Split(text, lf)
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, lf)
}

// ComputeFileHash returns the model-facing tag for a file's normalized
// (LF, BOM-free) content: the low 16 bits of FNV-1a/32 over the
// hash-normalized text, formatted as four uppercase hex digits.
//
// Caveat: Hashline syntax-compatible; tag is a Go-internal FNV
// fingerprint, not cross-language compatible. The 4-hex value is only the
// model-facing handle. Because it is just the low 16 bits, two different file
// versions can share a tag, so the patcher does not trust the tag alone: when
// the snapshot the tag was minted from is still recorded (every read/search/edit
// records one, §4.8), Patcher.Preflight takes the fast path only against an
// unambiguous base — the tag must pin to a single recorded content (UniqueByHash)
// equal to the live file. A live file that shares the tag but was never recorded,
// or a tag that two distinct recorded versions collide on, is rejected as stale.
// The tag is the cheap pre-check; unambiguous-base resolution is the backstop
// that makes a 16-bit collision unable to apply a stale patch.
func ComputeFileHash(text string) string {
	n := NormalizeForHash(text)
	h := fnv.New32a()
	// hash.Hash.Write never returns an error.
	_, _ = h.Write([]byte(n))
	return strings.ToUpper(fmt.Sprintf("%04X", uint16(h.Sum32()&0xFFFF)))
}
