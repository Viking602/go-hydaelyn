package eval

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestArtifactManifestSchema(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		want := ArtifactManifest{
			SchemaVersion: ArtifactManifestSchemaVersion,
			RunID:         "run-123",
			CreatedAt:     time.Date(2026, time.April, 19, 8, 9, 10, 0, time.UTC),
			Entries: []ArtifactManifestEntry{
				{
					ID:             "artifact-events",
					Kind:           ArtifactManifestKindEvents,
					Path:           "runs/run-123/events.ndjson",
					URI:            "file://runs/run-123/events.ndjson",
					Checksum:       "sha256:events123",
					Size:           2048,
					ContentType:    "application/x-ndjson",
					RetentionClass: RetentionClassLongTerm,
				},
				{
					ID:             "artifact-policy",
					Kind:           ArtifactManifestKindPolicyOutcomes,
					Path:           "runs/run-123/policy.json",
					URI:            "file://runs/run-123/policy.json",
					Checksum:       "sha256:policy123",
					Size:           512,
					ContentType:    "application/json",
					RetentionClass: RetentionClassPermanent,
					Redacted:       true,
				},
			},
		}

		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal artifact manifest: %v", err)
		}

		jsonText := string(data)
		for _, fragment := range []string{
			`"schemaVersion":"1.0"`,
			`"runId":"run-123"`,
			`"kind":"events"`,
			`"retentionClass":"permanent"`,
			`"redacted":true`,
		} {
			if !strings.Contains(jsonText, fragment) {
				t.Fatalf("expected marshaled JSON to contain %q, got %s", fragment, jsonText)
			}
		}

		var got ArtifactManifest
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal artifact manifest: %v", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})

	t.Run("omit optional sections", func(t *testing.T) {
		t.Parallel()

		manifest := ArtifactManifest{
			SchemaVersion: ArtifactManifestSchemaVersion,
			RunID:         "run-minimal",
			CreatedAt:     time.Date(2026, time.April, 19, 8, 0, 0, 0, time.UTC),
		}

		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("marshal minimal artifact manifest: %v", err)
		}

		if strings.Contains(string(data), `"entries"`) {
			t.Fatalf("expected marshaled JSON to omit entries, got %s", string(data))
		}
	})
}
