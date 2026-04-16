package blackboard

import (
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
