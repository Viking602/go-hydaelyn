package response

import (
	"regexp"
	"strings"
)

var emailRE = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// RedactUserPayload applies all user-payload redaction rules.
func RedactUserPayload(payload string) string {
	return RedactEmail(payload)
}

// RedactEmail replaces email addresses in payload with a redacted placeholder.
func RedactEmail(payload string) string {
	return emailRE.ReplaceAllString(payload, "[redacted-email]")
}

// HideInternalTrace removes lines that contain "internal trace" from payload.
func HideInternalTrace(payload string) string {
	lines := strings.Split(payload, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "internal trace") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
