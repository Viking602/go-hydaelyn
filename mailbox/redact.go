package mailbox

import (
	"regexp"
	"strings"
)

// piiPatterns mirrors the blackboard redaction set. Duplicated rather than
// imported so mailbox stays off of internal/blackboard (layering: mailbox is
// a top-level sibling package).
var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	regexp.MustCompile(`\b\d{3}[-.]?\d{2}[-.]?\d{4}\b`),
	regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`),
	regexp.MustCompile(`\b\+?1?[-.\s]?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),
}

const redactMarker = "[REDACTED]"

// redactBody strips common PII patterns from free-text content.
func redactBody(s string) string {
	if s == "" {
		return s
	}
	for _, p := range piiPatterns {
		s = p.ReplaceAllString(s, redactMarker)
	}
	return s
}

// normalizeLetter trims whitespace, redacts body and subject, and defaults
// missing Priority/Intent.
func normalizeLetter(l Letter) Letter {
	l.Subject = redactBody(strings.TrimSpace(l.Subject))
	l.Body = redactBody(strings.TrimSpace(l.Body))
	if l.Priority == "" {
		l.Priority = PriorityNormal
	}
	if l.Intent == "" {
		l.Intent = IntentAsk
	}
	return l
}

// bodyBytes returns the combined byte length used for oversize checks.
// Structured payloads and artifact refs aren't counted — they live elsewhere.
func bodyBytes(l Letter) int {
	return len(l.Subject) + len(l.Body)
}

// truncateForPrompt returns an inline-safe preview of a letter's body for
// prompt injection. If the body is oversized, it's truncated with an ellipsis
// and a size hint.
func truncateForPrompt(body string, max int) string {
	if max <= 0 || len(body) <= max {
		return body
	}
	if max < 16 {
		return body[:max]
	}
	return body[:max-3] + "..."
}
