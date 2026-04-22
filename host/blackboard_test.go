package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type verificationProvider struct{}

func (verificationProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "verification"}
}

func (verificationProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	if strings.HasSuffix(request.Metadata["taskId"], "-verify") {
		text := "supported"
		if strings.Contains(last, "reject branch") {
			text = "contradicted"
		}
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: text},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	if strings.Contains(request.Metadata["taskId"], "synth") {
		return provider.NewSliceStream(synthesisReportEvents(last)), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestPublishesBlackboardAndSynthesizesOnlyVerifiedClaims(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("verification", verificationProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "verification",
		Model:    "test",
	})
	runner.RegisterProfile(team.Profile{
		Name:     "researcher",
		Role:     team.RoleResearcher,
		Provider: "verification",
		Model:    "test",
	})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":               "verification flow",
			"subqueries":          []string{"keep branch", "reject branch"},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Blackboard == nil {
		t.Fatalf("expected blackboard state, got %#v", state)
	}
	if len(state.Blackboard.Claims) != 2 {
		t.Fatalf("expected published claims, got %#v", state.Blackboard)
	}
	if len(state.Blackboard.Verifications) != 2 {
		t.Fatalf("expected verification results, got %#v", state.Blackboard)
	}
	if state.Result == nil || len(state.Result.Findings) != 1 {
		t.Fatalf("expected only supported claim in final result, got %#v", state.Result)
	}
	if state.Result.Findings[0].Summary != "keep branch" {
		t.Fatalf("expected supported claim to survive synthesis, got %#v", state.Result.Findings)
	}
	if strings.Contains(state.Result.Summary, "reject branch") {
		t.Fatalf("expected contradicted claim to be excluded, got %#v", state.Result)
	}
}

func TestVerificationProviderUsesTaskMetadata(t *testing.T) {
	stream, err := (verificationProvider{}).Stream(context.Background(), provider.Request{
		Messages: []message.Message{message.NewText(message.RoleUser, "reject branch")},
		Metadata: map[string]string{"taskId": "task-1-verify"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if event.Text != "contradicted" {
		t.Fatalf("expected contradicted verification output, got %#v", event)
	}
}

func TestCollaborationBlackboard_GuardedSynthesisRequiresVerifiedClaims(t *testing.T) {
	// Under the selector-based model, VerifierRequired=true translates to
	// RequireVerified on every read. Exchanges no longer pass merely because
	// their namespace starts with "verify."; they must link to a claim that
	// has a SupportsClaim-qualifying verification (status=Supported,
	// confidence>=threshold, evidence-linked). This test captures that
	// stronger contract so the old namespace-prefix heuristic cannot sneak
	// back in.
	runner := New(Config{})
	board := &blackboard.State{
		Claims: []blackboard.Claim{{
			ID:     "claim-1",
			TaskID: "impl-1",
		}},
		Findings: []blackboard.Finding{{
			ID:       "finding-1",
			TaskID:   "impl-1",
			Summary:  "verified finding",
			ClaimIDs: []string{"claim-1"},
		}},
		Verifications: []blackboard.VerificationResult{{
			ClaimID:     "claim-1",
			Status:      blackboard.VerificationStatusSupported,
			Confidence:  0.95,
			EvidenceIDs: []string{"ev-1"},
		}},
	}
	for _, exchange := range []blackboard.Exchange{
		{
			Key:       "design.doc",
			Namespace: "impl.impl-1",
			TaskID:    "impl-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "raw implementation output",
		},
		{
			Key:       "design.doc",
			Namespace: "review.review-1",
			TaskID:    "review-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "review comments",
		},
		{
			Key:       "design.doc",
			Namespace: "verify.verify-1",
			TaskID:    "verify-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "verifier-approved output",
			ClaimIDs:  []string{"claim-1"},
		},
	} {
		if _, err := board.UpsertExchangeCAS(exchange); err != nil {
			t.Fatalf("UpsertExchangeCAS() error = %v", err)
		}
	}

	guardedTask := team.Task{
		ID:               "task-synthesize",
		Kind:             team.TaskKindSynthesize,
		Reads:            []string{"design.doc", "supported_findings"},
		VerifierRequired: true,
	}

	materialized, text := runner.materializeTaskInputs(team.RunState{Blackboard: board}, guardedTask)
	if len(materialized) != 1 {
		t.Fatalf("expected guarded synthesis to accept only the verified-claim-linked exchange, got %#v", materialized)
	}
	if materialized[0].Text != "verifier-approved output" || len(materialized[0].ClaimIDs) == 0 || materialized[0].ClaimIDs[0] != "claim-1" {
		t.Fatalf("expected the claim-1-linked exchange to be the only one materialized, got %#v", materialized)
	}
	if strings.Contains(text, "raw implementation output") || strings.Contains(text, "review comments") {
		t.Fatalf("expected guarded synthesis text to exclude unverified namespaces, got %q", text)
	}

	if _, err := board.UpsertExchangeCAS(blackboard.Exchange{
		Key:        "supported_findings",
		Namespace:  "verify.verify-1",
		TaskID:     "verify-1",
		Version:    1,
		ValueType:  blackboard.ExchangeValueTypeFindingRef,
		Text:       "verified finding",
		ClaimIDs:   []string{"claim-1"},
		FindingIDs: []string{"finding-1"},
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() verifier finding error = %v", err)
	}

	materialized, text = runner.materializeTaskInputs(team.RunState{Blackboard: board}, guardedTask)
	if len(materialized) != 2 {
		t.Fatalf("expected guarded synthesis to consume verified exchanges only, got %#v", materialized)
	}
	for _, item := range materialized {
		if len(item.ClaimIDs) == 0 || item.ClaimIDs[0] != "claim-1" {
			t.Fatalf("expected every materialized exchange to be linked to verified claim-1, got %#v", materialized)
		}
	}
	if strings.Contains(text, "raw implementation output") || strings.Contains(text, "review comments") {
		t.Fatalf("expected guarded synthesis text to exclude unverified exchanges, got %q", text)
	}
	if !strings.Contains(text, "verifier-approved output") || !strings.Contains(text, "verified finding") {
		t.Fatalf("expected guarded synthesis text to include verified exchanges, got %q", text)
	}
}

func TestMultiAgentCollaboration_VerifierPublishesSynthesisGate(t *testing.T) {
	runner := New(Config{})
	state := team.RunState{Blackboard: &blackboard.State{}}
	if _, err := state.Blackboard.UpsertExchangeCAS(blackboard.Exchange{
		Key:       "review.impl-api",
		Namespace: "review.impl-api",
		TaskID:    "impl-api-review",
		Version:   1,
		ValueType: blackboard.ExchangeValueTypeText,
		Text:      "reviewed implementation output",
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() review input error = %v", err)
	}
	state.Blackboard.Claims = []blackboard.Claim{{ID: "claim-1", TaskID: "impl-api-review"}}
	state.Blackboard.Findings = []blackboard.Finding{{
		ID:       "finding-1",
		TaskID:   "impl-api-review",
		Summary:  "reviewed implementation output",
		ClaimIDs: []string{"claim-1"},
	}}

	verifyTask := team.Task{
		ID:        "impl-api-verify",
		Kind:      team.TaskKindVerify,
		Stage:     team.TaskStageVerify,
		Namespace: "verify.impl-api",
		Version:   1,
		Reads:     []string{"review.impl-api"},
		Writes:    []string{"verify.impl-api"},
		Publish:   []team.OutputVisibility{team.OutputVisibilityBlackboard},
		DependsOn: []string{"impl-api-review"},
		Result: &team.Result{
			Summary:    "supported",
			Confidence: 0.9,
		},
	}

	state = runner.applyBlackboardUpdate(state, verifyTask)
	exchanges := state.Blackboard.ExchangesForTask(verifyTask.ID)
	if len(exchanges) == 0 {
		t.Fatalf("expected verifier exchanges, got %#v", state.Blackboard)
	}

	var gate *blackboard.Exchange
	var published *blackboard.Exchange
	for idx := range exchanges {
		exchange := exchanges[idx]
		switch exchange.Key {
		case verifierGateExchangeKey:
			gate = &exchange
		case "verify.impl-api":
			published = &exchange
		}
	}
	if gate == nil {
		t.Fatalf("expected explicit verifier gate exchange, got %#v", exchanges)
	}
	if gate.Namespace != "verify.impl-api" {
		t.Fatalf("expected verifier gate namespace to stay under verify.*, got %#v", gate)
	}
	if decision := gate.Metadata[verifierGateDecisionField]; decision != verifierGatePassDecision {
		t.Fatalf("expected pass decision metadata, got %#v", gate.Metadata)
	}
	if status := gate.Metadata[verifierGateStatusField]; status != string(blackboard.VerificationStatusSupported) {
		t.Fatalf("expected supported status metadata, got %#v", gate.Metadata)
	}
	if count, ok := gate.Structured[verifierGateEvidenceCountField].(int); !ok || count != 1 {
		t.Fatalf("expected consumed published input count, got %#v", gate.Structured)
	}
	if published == nil {
		t.Fatalf("expected verifier write exchange, got %#v", exchanges)
	}
	if published.Namespace != "verify.impl-api" {
		t.Fatalf("expected published verifier output in verify namespace, got %#v", published)
	}
	if published.Metadata[verifierGateDecisionField] != verifierGatePassDecision {
		t.Fatalf("expected synthesis gate metadata on verifier output, got %#v", published.Metadata)
	}
	if published.Structured[verifierGateStatusField] != string(blackboard.VerificationStatusSupported) {
		t.Fatalf("expected structured verification status on verifier output, got %#v", published.Structured)
	}
	if decision, status, ok := verifierGateEvidence(state.Blackboard, verifyTask); !ok || decision != verifierGatePassDecision || status != string(blackboard.VerificationStatusSupported) {
		t.Fatalf("expected verifier evidence lookup to resolve explicit gate, got decision=%q status=%q ok=%v", decision, status, ok)
	}
	if len(state.Blackboard.ExchangesForKey("supported_findings")) != 1 {
		t.Fatalf("expected supported findings compatibility exchange, got %#v", state.Blackboard.ExchangesForKey("supported_findings"))
	}
}

func TestMultiAgentCollaboration_VerifierBlocksSynthesisOnMissingEvidence(t *testing.T) {
	runner := New(Config{})
	state := team.RunState{
		ID:     "team-1",
		Status: team.StatusRunning,
		Tasks: []team.Task{
			{
				ID:        "impl-api-verify",
				Kind:      team.TaskKindVerify,
				Stage:     team.TaskStageVerify,
				Namespace: "verify.impl-api",
				Status:    team.TaskStatusCompleted,
			},
			{
				ID:               "task-synthesize",
				Kind:             team.TaskKindSynthesize,
				Stage:            team.TaskStageSynthesize,
				AssigneeAgentID:  "supervisor",
				DependsOn:        []string{"impl-api-verify"},
				Reads:            []string{"verify.impl-api"},
				VerifierRequired: true,
				Status:           team.TaskStatusPending,
			},
		},
	}

	next, err := runner.executeTasks(context.Background(), state)
	if err != nil {
		t.Fatalf("executeTasks() error = %v", err)
	}
	synth := next.Tasks[1]
	if synth.Status != team.TaskStatusFailed {
		t.Fatalf("expected guarded synthesis to fail without verifier evidence, got %#v", synth)
	}
	if !strings.Contains(synth.Error, "missing verifier evidence") {
		t.Fatalf("expected missing verifier evidence error, got %#v", synth)
	}

	next.Blackboard = &blackboard.State{}
	if _, err := next.Blackboard.UpsertExchangeCAS(blackboard.Exchange{
		Key:       verifierGateExchangeKey,
		Namespace: "verify.impl-api",
		TaskID:    "impl-api-verify",
		Version:   1,
		ValueType: blackboard.ExchangeValueTypeJSON,
		Structured: map[string]any{
			verifierGateDecisionField: verifierGateBlockDecision,
			verifierGateStatusField:   string(blackboard.VerificationStatusContradicted),
		},
		Metadata: map[string]string{
			verifierGateDecisionField: verifierGateBlockDecision,
			verifierGateStatusField:   string(blackboard.VerificationStatusContradicted),
		},
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() blocked gate error = %v", err)
	}
	if reason, blocked := synthesisVerifierBlockReason(next, next.Tasks[1]); !blocked || !strings.Contains(reason, "blocked by verifier") {
		t.Fatalf("expected contradicted verifier evidence to block synthesis, got reason=%q blocked=%v", reason, blocked)
	}
}

func TestVerifierStructuredClaimsOverrideSummaryHeuristics(t *testing.T) {
	runner := New(Config{})
	state := team.RunState{
		Blackboard: &blackboard.State{
			Claims: []blackboard.Claim{
				{ID: "claim-1", TaskID: "impl-1", EvidenceIDs: []string{"evidence-1"}},
				{ID: "claim-2", TaskID: "impl-1", EvidenceIDs: []string{"evidence-2"}},
			},
			Findings: []blackboard.Finding{
				{ID: "finding-1", TaskID: "impl-1", Summary: "supported claim", ClaimIDs: []string{"claim-1"}},
				{ID: "finding-2", TaskID: "impl-1", Summary: "unsupported claim", ClaimIDs: []string{"claim-2"}},
			},
		},
	}

	verifyTask := team.Task{
		ID:        "verify-1",
		Kind:      team.TaskKindVerify,
		Namespace: "verify.impl-1",
		Version:   1,
		DependsOn: []string{"impl-1"},
		Writes:    []string{"verify.impl-1"},
		Publish:   []team.OutputVisibility{team.OutputVisibilityBlackboard},
		Result: &team.Result{
			Summary: "supported",
			Structured: map[string]any{
				"claims": []any{
					map[string]any{
						"claimId":     "claim-1",
						"decision":    "supported",
						"confidence":  0.92,
						"evidenceIds": []any{"evidence-1"},
					},
					map[string]any{
						"claimId":    "claim-2",
						"decision":   "unsupported",
						"confidence": 0.88,
					},
				},
			},
		},
	}

	state = runner.applyBlackboardUpdate(state, verifyTask)

	if got := len(state.Blackboard.Verifications); got != 2 {
		t.Fatalf("expected two claim-level verifications, got %#v", state.Blackboard.Verifications)
	}
	if state.Blackboard.Verifications[0].ClaimID != "claim-1" || state.Blackboard.Verifications[0].Status != blackboard.VerificationStatusSupported {
		t.Fatalf("expected structured support result for claim-1, got %#v", state.Blackboard.Verifications[0])
	}
	if state.Blackboard.Verifications[1].ClaimID != "claim-2" || state.Blackboard.Verifications[1].Status != blackboard.VerificationStatusContradicted {
		t.Fatalf("expected structured contradiction result for claim-2, got %#v", state.Blackboard.Verifications[1])
	}

	supported := state.Blackboard.ExchangesForKey("supported_findings")
	if len(supported) != 1 || len(supported[0].ClaimIDs) != 1 || supported[0].ClaimIDs[0] != "claim-1" {
		t.Fatalf("expected only supported evidence-backed claim to publish supported finding, got %#v", supported)
	}

	// Under the strict gate rules a single contradicted claim short-circuits
	// the whole gate — we must not let a supported sibling drown out a
	// contradiction when deciding whether synthesis may proceed.
	if decision, status, ok := verifierGateEvidence(state.Blackboard, verifyTask); !ok || decision != verifierGateBlockDecision || status != string(blackboard.VerificationStatusContradicted) {
		t.Fatalf("expected mixed claim-level results to block on the contradicted claim, got decision=%q status=%q ok=%v", decision, status, ok)
	}
}

func TestVerifierTypedReportRejectsOutOfScopeClaimIDs(t *testing.T) {
	runner := New(Config{})
	state := team.RunState{
		Blackboard: &blackboard.State{
			Claims: []blackboard.Claim{
				{ID: "claim-1", TaskID: "impl-1", EvidenceIDs: []string{"evidence-1"}},
			},
		},
	}
	verifyTask := team.Task{
		ID:        "verify-1",
		Kind:      team.TaskKindVerify,
		Namespace: "verify.impl-1",
		Version:   1,
		DependsOn: []string{"impl-1"},
		Writes:    []string{"verify.impl-1"},
		Publish:   []team.OutputVisibility{team.OutputVisibilityBlackboard},
		Result: &team.Result{
			Summary: "supported",
			Structured: map[string]any{
				team.ReportKey: map[string]any{
					"kind":   string(team.ReportKindVerification),
					"status": string(team.VerificationStatusSupported),
					"perClaim": []any{
						map[string]any{
							"claimId":     "claim-does-not-exist",
							"status":      string(team.VerificationStatusSupported),
							"confidence":  0.95,
							"evidenceIds": []any{"evidence-1"},
						},
					},
				},
			},
		},
	}

	state = runner.applyBlackboardUpdate(state, verifyTask)

	if got := len(state.Blackboard.Verifications); got != 0 {
		t.Fatalf("expected malformed typed verification report to publish no claim results, got %#v", state.Blackboard.Verifications)
	}
	if supported := state.Blackboard.ExchangesForKey("supported_findings"); len(supported) != 0 {
		t.Fatalf("expected malformed typed verification report to publish no supported findings, got %#v", supported)
	}
	if decision, status, ok := verifierGateEvidence(state.Blackboard, verifyTask); !ok || decision != verifierGateBlockDecision || status != string(blackboard.VerificationStatusInsufficient) {
		t.Fatalf("expected malformed typed verification report to block synthesis, got decision=%q status=%q ok=%v", decision, status, ok)
	}
}
