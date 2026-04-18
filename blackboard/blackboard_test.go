package blackboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPIIRedaction(t *testing.T) {
	pipeline := NewPipeline()
	state := &State{}
	pipeline.Publish(state, PublishRequest{
		TaskID:     "task-pii",
		Title:      "PII test",
		Summary:    "Contact user@example.com or call +1-555-123-4567, SSN 123-45-6789, card 4111 1111 1111 1111",
		Confidence: 0.9,
	})
	summary := state.Claims[0].Summary
	if strings.Contains(summary, "user@example.com") {
		t.Fatalf("email not redacted in %q", summary)
	}
	if strings.Contains(summary, "555-123-4567") {
		t.Fatalf("phone not redacted in %q", summary)
	}
	if strings.Contains(summary, "123-45-6789") {
		t.Fatalf("SSN not redacted in %q", summary)
	}
	if strings.Contains(summary, "4111") {
		t.Fatalf("credit card not redacted in %q", summary)
	}
}

func TestPipelinePublishNormalizesDedupesRedactsAndScores(t *testing.T) {
	pipeline := NewPipeline()
	state := &State{}

	first := pipeline.Publish(state, PublishRequest{
		TaskID:     "task-1",
		Title:      "Doc A",
		Summary:    "  API key sk-secret1234567890 is mentioned here  ",
		Confidence: 0.8,
		Evidence: []EvidenceInput{
			{
				Source:  "Doc A",
				Snippet: " token sk-secret1234567890 ",
			},
		},
	})
	second := pipeline.Publish(state, PublishRequest{
		TaskID:     "task-1",
		Title:      "Doc A",
		Summary:    "API key sk-secret1234567890 is mentioned here",
		Confidence: 0.8,
		Evidence: []EvidenceInput{
			{
				Source:  "Doc A",
				Snippet: "token sk-secret1234567890",
			},
		},
	})

	if len(state.Claims) != 1 {
		t.Fatalf("expected deduped claim, got %#v", state.Claims)
	}
	if len(state.Evidence) != 1 {
		t.Fatalf("expected deduped evidence, got %#v", state.Evidence)
	}
	if len(state.Sources) != 1 || len(state.Artifacts) != 1 || len(state.Findings) != 1 {
		t.Fatalf("expected source/artifact/finding to be published, got %#v", state)
	}
	if first.ClaimID != second.ClaimID || first.EvidenceIDs[0] != second.EvidenceIDs[0] {
		t.Fatalf("expected duplicate publish to reuse ids, first=%#v second=%#v", first, second)
	}
	if strings.Contains(state.Claims[0].Summary, "sk-secret") {
		t.Fatalf("expected claim summary to be redacted, got %#v", state.Claims[0])
	}
	if strings.Contains(state.Evidence[0].Snippet, "sk-secret") {
		t.Fatalf("expected evidence snippet to be redacted, got %#v", state.Evidence[0])
	}
	if state.Evidence[0].Score <= 0 || state.Findings[0].Confidence <= 0 {
		t.Fatalf("expected publish pipeline to score evidence/finding, got %#v", state)
	}
}

func TestStateUpsertExchangeDedupesAndSupportsDeterministicKeyReads(t *testing.T) {
	state := &State{}

	first := state.UpsertExchange(Exchange{
		Key:       "research.branch",
		TaskID:    "task-1",
		ValueType: ExchangeValueTypeJSON,
		Text:      "branch summary",
		Structured: map[string]any{
			"summary": "branch summary",
		},
		ArtifactIDs: []string{"artifact-1"},
		Metadata:    map[string]string{"phase": "research"},
	})
	second := state.UpsertExchange(Exchange{
		Key:       "research.branch",
		TaskID:    "task-1",
		ValueType: ExchangeValueTypeJSON,
		Text:      "branch summary",
		Structured: map[string]any{
			"summary": "branch summary",
		},
		ArtifactIDs: []string{"artifact-1"},
		Metadata:    map[string]string{"phase": "research"},
	})
	state.UpsertExchange(Exchange{
		Key:        "supported_findings",
		TaskID:     "task-1-verify",
		ValueType:  ExchangeValueTypeFindingRef,
		Text:       "branch summary",
		FindingIDs: []string{"finding-task-1-1"},
	})

	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected exchange ids, got first=%#v second=%#v", first, second)
	}
	if first.ID != second.ID {
		t.Fatalf("expected duplicate upsert to reuse id, got first=%#v second=%#v", first, second)
	}
	if len(state.Exchanges) != 2 {
		t.Fatalf("expected deduped exchanges, got %#v", state.Exchanges)
	}

	branch := state.ExchangesForKey("research.branch")
	if len(branch) != 1 {
		t.Fatalf("expected one exchange for research.branch, got %#v", branch)
	}
	if branch[0].ValueType != ExchangeValueTypeJSON {
		t.Fatalf("expected json exchange, got %#v", branch[0])
	}
	if !reflect.DeepEqual(branch[0].ArtifactIDs, []string{"artifact-1"}) {
		t.Fatalf("expected artifact refs to survive, got %#v", branch[0])
	}

	supported := state.ExchangesForKey("supported_findings")
	if len(supported) != 1 || supported[0].Text != "branch summary" {
		t.Fatalf("expected supported finding exchange, got %#v", supported)
	}
}

func TestCollaborationBlackboard_RejectsStaleExchangeWrite(t *testing.T) {
	state := &State{}

	current, err := state.UpsertExchangeCAS(Exchange{
		Key:       "branch.report",
		Namespace: "impl.task-1",
		TaskID:    "task-1",
		Version:   2,
		ValueType: ExchangeValueTypeJSON,
		Text:      "fresh summary",
		Structured: map[string]any{
			"summary": "fresh summary",
		},
	})
	if err != nil {
		t.Fatalf("UpsertExchangeCAS() unexpected error = %v", err)
	}
	if current.ETag == "" {
		t.Fatalf("expected CAS upsert to assign etag, got %#v", current)
	}

	if _, err := state.UpsertExchangeCAS(Exchange{
		Key:       "branch.report",
		Namespace: "impl.task-1",
		TaskID:    "task-1",
		Version:   1,
		ValueType: ExchangeValueTypeJSON,
		Text:      "stale summary",
		Structured: map[string]any{
			"summary": "stale summary",
		},
	}); !errors.Is(err, ErrExchangeConflict) {
		t.Fatalf("expected stale write conflict, got %v", err)
	}

	items := state.ExchangesForKey("branch.report")
	if len(items) != 1 {
		t.Fatalf("expected authoritative exchange to remain singular, got %#v", items)
	}
	if items[0].Text != "fresh summary" {
		t.Fatalf("expected stale write to leave authoritative exchange unchanged, got %#v", items[0])
	}
	if items[0].Version != 2 || items[0].Namespace != "impl.task-1" || items[0].ETag == "" {
		t.Fatalf("expected namespaced CAS metadata to remain intact, got %#v", items[0])
	}
}
