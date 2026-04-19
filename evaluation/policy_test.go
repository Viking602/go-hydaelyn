package evaluation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPolicyOutcomeSchema(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		want := PolicyOutcome{
			SchemaVersion: PolicyOutcomeSchemaVersion,
			Policy:        "groundedness",
			Outcome:       "blocked",
			Severity:      "high",
			Message:       "required citation missing for one claim",
			Blocking:      true,
			Reference:     "policy://groundedness",
			Timestamp:     time.Date(2026, time.April, 19, 9, 10, 11, 0, time.UTC),
			Evidence: &PolicyOutcomeEvidence{
				ArtifactIDs:    []string{"artifact-events", "artifact-score"},
				EventSequences: []int{4, 9},
				Excerpt:        "claim lacked supporting evidence",
				Metadata: map[string]string{
					"claimId": "claim-7",
				},
			},
		}

		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal policy outcome: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{
			`"schemaVersion":"1.0"`,
			`"policy":"groundedness"`,
			`"blocking":true`,
			`"timestamp":"2026-04-19T09:10:11Z"`,
			`"artifactIds":[`,
		} {
			if !strings.Contains(jsonText, fragment) {
				t.Fatalf("expected marshaled JSON to contain %q, got %s", fragment, jsonText)
			}
		}

		var got PolicyOutcome
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal policy outcome: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})

	t.Run("omit optional sections", func(t *testing.T) {
		t.Parallel()

		outcome := PolicyOutcome{
			SchemaVersion: PolicyOutcomeSchemaVersion,
			Policy:        "safety",
			Timestamp:     time.Date(2026, time.April, 19, 9, 0, 0, 0, time.UTC),
		}

		data, err := json.Marshal(outcome)
		if err != nil {
			t.Fatalf("marshal minimal policy outcome: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{"outcome", "severity", "message", "reference", "evidence", "blocking"} {
			if strings.Contains(jsonText, `"`+fragment+`"`) {
				t.Fatalf("expected marshaled JSON to omit %q, got %s", fragment, jsonText)
			}
		}
	})
}
