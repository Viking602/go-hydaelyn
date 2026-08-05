package response_test

import (
	"testing"

	"github.com/Viking602/venat/internal/response"
)

func TestRedactionHelpersRemoveEmailAndInternalTrace(t *testing.T) {
	payload := "send to owner@example.com\ninternal trace: span-1\nkeep this"
	redacted := response.HideInternalTrace(response.RedactUserPayload(payload))
	if redacted != "send to [redacted-email]\nkeep this" {
		t.Fatalf("redacted payload = %q", redacted)
	}
}
