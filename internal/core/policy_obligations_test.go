package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
)

type obligationPolicyFunc func(context.Context, model.PolicyRequest) (model.PolicyDecision, error)

func (f obligationPolicyFunc) Authorize(ctx context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
	return f(ctx, request)
}

func TestBlackboardReadObligationsFilterAndRedact(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-blackboard-policy", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	for _, item := range []model.BlackboardItem{
		{RunID: run.ID, Key: "public", Content: "owner@example.com", Payload: "owner@example.com", Visibility: model.BlackboardVisibilityAgentVisible},
		{RunID: run.ID, Key: "private", Content: "secret@example.com", Payload: "secret@example.com", Visibility: model.BlackboardVisibilityInternal},
	} {
		if err := rt.WriteItem(ctx, item); err != nil {
			t.Fatalf("WriteItem(%q) error = %v", item.Key, err)
		}
	}
	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationBlackboardRead {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID: "decision-blackboard-read",
			Effect:     model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{
				{
					Kind:     model.ObligationSelectorOnly,
					Target:   model.PolicyTargetBlackboardRead,
					Selector: &model.BlackboardSelector{Keys: []string{"public"}},
				},
				{Kind: model.ObligationRedactFields, Target: model.PolicyTargetBlackboardRead},
			},
			Redactions: []string{"email"},
		}, nil
	}))

	items, err := rt.SelectItems(ctx, run.ID, model.BlackboardSelector{})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Key != "public" {
		t.Fatalf("SelectItems() = %#v, want only public item", items)
	}
	if strings.Contains(items[0].Content, "owner@example.com") || strings.Contains(items[0].Payload, "owner@example.com") {
		t.Fatalf("SelectItems() leaked unredacted item: %#v", items[0])
	}
	var audited model.Event
	for _, event := range rt.Events(ctx, run.ID) {
		if event.Type == model.EventPolicyDecisionRecorded &&
			stringFromPayload(event.Payload["decisionId"]) == "decision-blackboard-read" {
			audited = event
			break
		}
	}
	if audited.Type == "" {
		t.Fatalf("missing PolicyDecisionRecorded event: %#v", rt.Events(ctx, run.ID))
	}
	rawObligations, err := json.Marshal(audited.Payload["obligations"])
	if err != nil {
		t.Fatal(err)
	}
	var auditedObligations []model.PolicyObligation
	if err := json.Unmarshal(rawObligations, &auditedObligations); err != nil {
		t.Fatal(err)
	}
	if len(auditedObligations) != 2 || auditedObligations[0].Selector == nil ||
		len(auditedObligations[0].Selector.Keys) != 1 ||
		auditedObligations[0].Selector.Keys[0] != "public" {
		t.Fatalf("audited obligations dropped selector: %#v", auditedObligations)
	}
}

func TestBlackboardWriteObligationsTransformOrFailClosed(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-blackboard-write-policy", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationBlackboardWrite {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID:  "decision-blackboard-write",
			Effect:      model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{{Kind: model.ObligationRedactFields, Target: model.PolicyTargetBlackboardWrite}},
			Redactions:  []string{"email"},
		}, nil
	}))
	if err := rt.WriteItem(ctx, model.BlackboardItem{
		RunID: run.ID, Key: "redacted", Content: "owner@example.com", Payload: "owner@example.com",
	}); err != nil {
		t.Fatalf("WriteItem(redacted) error = %v", err)
	}

	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationBlackboardWrite {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID: "decision-blackboard-deny",
			Effect:     model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{{
				Kind:     model.ObligationSelectorOnly,
				Target:   model.PolicyTargetBlackboardWrite,
				Selector: &model.BlackboardSelector{Keys: []string{"allowed"}},
			}},
		}, nil
	}))
	if err := rt.WriteItem(ctx, model.BlackboardItem{RunID: run.ID, Key: "blocked"}); !errors.Is(err, model.ErrPolicyObligationFailed) {
		t.Fatalf("WriteItem(blocked) error = %v, want ErrPolicyObligationFailed", err)
	}

	rt.SetPolicyEngine(nil)
	items, err := rt.SelectItems(ctx, run.ID, model.BlackboardSelector{Keys: []string{"redacted", "blocked"}})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Key != "redacted" || strings.Contains(items[0].Payload, "owner@example.com") {
		t.Fatalf("stored blackboard items = %#v", items)
	}
	if !collectEventTypes(rt.Events(ctx, run.ID)).Contains(model.EventPolicyObligationFailed) {
		t.Fatalf("missing PolicyObligationFailed event: %#v", rt.Events(ctx, run.ID))
	}
}

func TestHandoffObligationRemovesContextBeforePersistence(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-handoff-policy", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationHandoff {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID: "decision-restrict-handoff",
			Effect:     model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{{
				Kind:   model.ObligationRestrictHandoffContext,
				Target: model.PolicyTargetHandoff,
			}},
		}, nil
	}))
	if err := rt.RequestHandoff(ctx, HandoffCommand{
		RunID:          run.ID,
		TaskID:         task.ID,
		FromAgentID:    "agent-a",
		ToAgentID:      "agent-b",
		TaskVersion:    task.Version,
		HandoffContext: "private-token",
	}); err != nil {
		t.Fatalf("RequestHandoff() error = %v", err)
	}
	updated, err := rt.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if updated.OwnerAgentID != "agent-b" {
		t.Fatalf("handoff owner = %q, want agent-b", updated.OwnerAgentID)
	}
	items, err := rt.SelectItems(ctx, run.ID, model.BlackboardSelector{})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	for _, item := range items {
		if item.Type == model.BlackboardItemHandoffContext || strings.Contains(item.Content, "private-token") || strings.Contains(item.Payload, "private-token") {
			t.Fatalf("handoff context leaked to blackboard: %#v", item)
		}
	}
}

func TestTraceReadObligationsHideOrRedactSpans(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-trace-policy", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if err := rt.SaveTraceSpan(ctx, model.TraceSpan{
		ID: "span-sensitive", RunID: run.ID, Name: "sensitive", Error: "owner@example.com",
		Metadata: map[string]string{"secret": "token", "contact": "owner@example.com"},
	}); err != nil {
		t.Fatalf("SaveTraceSpan() error = %v", err)
	}
	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationTraceRead {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID: "decision-hide-trace",
			Effect:     model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{{
				Kind: model.ObligationHideInternalTrace, Target: model.PolicyTargetTrace,
			}},
		}, nil
	}))
	spans, err := rt.ListTraceSpans(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListTraceSpans(hide) error = %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("ListTraceSpans(hide) = %#v, want no visible spans", spans)
	}

	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationTraceRead {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			DecisionID:  "decision-redact-trace",
			Effect:      model.PolicyEffectAllow,
			Obligations: []model.PolicyObligation{{Kind: model.ObligationRedactFields, Target: model.PolicyTargetTrace}},
			Redactions:  []string{"email", "metadata.secret"},
		}, nil
	}))
	spans, err = rt.ListTraceSpans(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListTraceSpans(redact) error = %v", err)
	}
	for _, span := range spans {
		if span.ID != "span-sensitive" {
			continue
		}
		if strings.Contains(span.Error, "owner@example.com") || strings.Contains(span.Metadata["contact"], "owner@example.com") {
			t.Fatalf("trace email was not redacted: %#v", span)
		}
		if _, exists := span.Metadata["secret"]; exists {
			t.Fatalf("trace secret metadata was not removed: %#v", span)
		}
		return
	}
	t.Fatalf("redacted trace span not returned: %#v", spans)
}

func TestPolicyDecisionAuditAndApprovalObligation(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-policy-audit", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		if request.Operation != model.PolicyOperationDispatch {
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
		return model.PolicyDecision{
			Effect: model.PolicyEffectAllow,
			Reason: "human review required",
			Obligations: []model.PolicyObligation{{
				Kind: model.ObligationRequireHumanApproval,
			}},
			Metadata: map[string]string{"rule": "review"},
		}, nil
	}))
	if _, err := rt.DispatchTask(ctx, DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	}); !errors.Is(err, model.ErrPolicyDenied) {
		t.Fatalf("DispatchTask() error = %v, want ErrPolicyDenied", err)
	}

	var audited model.Event
	for _, event := range rt.Events(ctx, run.ID) {
		if event.Type == model.EventPolicyDecisionRecorded &&
			stringFromPayload(event.Payload["operation"]) == string(model.PolicyOperationDispatch) {
			audited = event
			break
		}
	}
	if audited.Type == "" {
		t.Fatalf("missing PolicyDecisionRecorded event: %#v", rt.Events(ctx, run.ID))
	}
	if stringFromPayload(audited.Payload["decisionId"]) == "" ||
		stringFromPayload(audited.Payload["effect"]) != string(model.PolicyEffectRequireApproval) {
		t.Fatalf("policy audit payload = %#v", audited.Payload)
	}
	if !collectEventTypes(rt.Events(ctx, run.ID)).Contains(model.EventApprovalRequested) {
		t.Fatalf("missing ApprovalRequested event: %#v", rt.Events(ctx, run.ID))
	}
}

func TestPolicyEngineErrorFailsClosedAndPersistsAudit(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-policy-error", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	engineErr := errors.New("policy backend unavailable")
	rt.SetPolicyEngine(obligationPolicyFunc(func(context.Context, model.PolicyRequest) (model.PolicyDecision, error) {
		return model.PolicyDecision{}, engineErr
	}))
	err = rt.WriteItem(ctx, model.BlackboardItem{RunID: run.ID, Key: "blocked"})
	if !errors.Is(err, model.ErrPolicyDenied) || !strings.Contains(err.Error(), engineErr.Error()) {
		t.Fatalf("WriteItem() error = %v, want fail-closed policy error", err)
	}
	for _, event := range rt.Events(ctx, run.ID) {
		if event.Type != model.EventPolicyDecisionRecorded {
			continue
		}
		if stringFromPayload(event.Payload["effect"]) != string(model.PolicyEffectDeny) ||
			stringFromPayload(event.Payload["decisionId"]) == "" {
			t.Fatalf("policy error audit payload = %#v", event.Payload)
		}
		return
	}
	t.Fatalf("missing policy error audit event: %#v", rt.Events(ctx, run.ID))
}

func TestToolResultEmailRedactionTraversesStructuredJSON(t *testing.T) {
	decision := model.PolicyDecision{
		Effect:      model.PolicyEffectAllow,
		Redactions:  []string{"email"},
		Obligations: []model.PolicyObligation{{Kind: model.ObligationRedactFields, Target: model.PolicyTargetToolResult}},
	}
	result := json.RawMessage(`{
		"toolCallId":"call-1",
		"name":"lookup",
		"content":"contact owner@example.com",
		"structured":{
			"profile":{"email":"owner@example.com","count":7,"enabled":true,"missing":null},
			"items":["other@example.com",3,false,null,{"note":"nested@example.com"}]
		},
		"extra":{"preserve":"yes"}
	}`)

	enforced, err := (defaultPolicyObligationEnforcer{}).EnforceToolResult(context.Background(), decision, result)
	if err != nil {
		t.Fatalf("EnforceToolResult() error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(enforced, &fields); err != nil {
		t.Fatalf("enforced result is not a JSON object: %v", err)
	}
	var content string
	if err := json.Unmarshal(fields["content"], &content); err != nil {
		t.Fatalf("content is not a string: %v", err)
	}
	if content != "contact [redacted-email]" {
		t.Fatalf("content = %q, want email redaction", content)
	}
	var structured struct {
		Profile map[string]json.RawMessage `json:"profile"`
		Items   []json.RawMessage          `json:"items"`
	}
	if err := json.Unmarshal(fields["structured"], &structured); err != nil {
		t.Fatalf("structured is not valid JSON: %v", err)
	}
	var email string
	if err := json.Unmarshal(structured.Profile["email"], &email); err != nil {
		t.Fatalf("profile email is not a string: %v", err)
	}
	if email != "[redacted-email]" {
		t.Fatalf("profile email = %q, want redaction", email)
	}
	if got := string(structured.Profile["count"]); got != "7" {
		t.Fatalf("profile count = %s, want numeric scalar preserved", got)
	}
	if got := string(structured.Profile["enabled"]); got != "true" {
		t.Fatalf("profile enabled = %s, want boolean scalar preserved", got)
	}
	if got := string(structured.Profile["missing"]); got != "null" {
		t.Fatalf("profile missing = %s, want null scalar preserved", got)
	}
	wantItems := []string{`"[redacted-email]"`, "3", "false", "null"}
	for index, want := range wantItems {
		if got := string(structured.Items[index]); got != want {
			t.Fatalf("structured items[%d] = %s, want %s", index, got, want)
		}
	}
	var nestedNote string
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(structured.Items[4], &nested); err != nil {
		t.Fatalf("nested object is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(nested["note"], &nestedNote); err != nil {
		t.Fatalf("nested note is not a string: %v", err)
	}
	if nestedNote != "[redacted-email]" {
		t.Fatalf("nested note = %q, want redaction", nestedNote)
	}
	if string(fields["extra"]) != `{"preserve":"yes"}` {
		t.Fatalf("extra field = %s, want preserved", fields["extra"])
	}
}

func TestToolResultEmailRedactionFailsClosedOnMalformedStructuredJSON(t *testing.T) {
	decision := model.PolicyDecision{
		Effect:      model.PolicyEffectAllow,
		Redactions:  []string{"email"},
		Obligations: []model.PolicyObligation{{Kind: model.ObligationRedactFields, Target: model.PolicyTargetToolResult}},
	}
	_, err := (defaultPolicyObligationEnforcer{}).EnforceToolResult(
		context.Background(),
		decision,
		json.RawMessage(`{"content":"owner@example.com","structured":{"nested":[}`),
	)
	if !errors.Is(err, model.ErrPolicyObligationFailed) {
		t.Fatalf("EnforceToolResult() error = %v, want ErrPolicyObligationFailed", err)
	}
}
