package hashline

import "strings"

// utf8BOM is the UTF-8 byte-order-mark sequence (EF BB BF), written as an
// escape so the source file itself carries no literal BOM.
const utf8BOM = "\uFEFF"

// crlf and lf are the two line endings the protocol round-trips.
const (
	crlf = "\r\n"
	lf   = "\n"
)

// NormalizedFile is the result of normalizing raw file bytes into the
// internal LF-only, BOM-free representation the rest of the package works
// with. The original BOM and line ending are recorded so a commit can
// restore the file's on-disk shape exactly.
type NormalizedFile struct {
	// Raw is the original, unmodified input text.
	Raw string
	// BOM is the stripped leading byte-order mark, either "" or utf8BOM.
	BOM string
	// LineEnding is the detected ending used on restore: "\n" or "\r\n".
	LineEnding string
	// Text is the normalized content: LF line endings, no leading BOM.
	Text string
}

// Normalize detects and strips a leading UTF-8 BOM, detects the line
// ending (CRLF if any "\r\n" is present, otherwise LF), and produces an
// internal Text that always uses LF with no BOM. A trailing CR not part of
// a CRLF pair is left intact (it is treated as ordinary content).
func Normalize(raw string) NormalizedFile {
	nf := NormalizedFile{Raw: raw}

	body := raw
	if strings.HasPrefix(body, utf8BOM) {
		nf.BOM = utf8BOM
		body = body[len(utf8BOM):]
	}

	if strings.Contains(body, crlf) {
		nf.LineEnding = crlf
	} else {
		nf.LineEnding = lf
	}

	// Normalize CRLF to LF for the internal representation. Lone CRs (not
	// followed by LF) are preserved verbatim as content.
	nf.Text = strings.ReplaceAll(body, crlf, lf)
	return nf
}

// Restore re-applies the recorded BOM and line ending to LF-internal text,
// producing bytes suitable to write back to disk. It is the inverse of
// Normalize for the BOM and line-ending dimensions.
func (nf NormalizedFile) Restore(text string) string {
	out := text
	if nf.LineEnding == crlf {
		// Collapse any pre-existing CRLF first so we never double-convert,
		// then expand every LF to CRLF.
		out = strings.ReplaceAll(out, crlf, lf)
		out = strings.ReplaceAll(out, lf, crlf)
	}
	return nf.BOM + out
}
