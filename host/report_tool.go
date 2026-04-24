package host

import (
	"context"
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

const submitReportToolName = "submit_report"

type submitReportTool struct {
	kind team.ReportKind
}

func (t submitReportTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        submitReportToolName,
		Description: "Submit the structured report for this task instead of writing JSON in assistant text.",
		Terminal:    true,
		InputSchema: reportSchemaForKind(t.kind),
	}
}

func (t submitReportTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(call.Arguments, &payload); err != nil {
		return tool.Result{}, err
	}
	if _, ok := payload["kind"]; !ok {
		payload["kind"] = string(t.kind)
	}
	structured, err := json.Marshal(map[string]any{
		team.ReportKey: payload,
	})
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Structured: structured,
	}, nil
}

func reportKindForTask(task team.Task) (team.ReportKind, bool) {
	if task.ExpectedReportKind != "" {
		return task.ExpectedReportKind, true
	}
	switch task.Kind {
	case team.TaskKindSynthesize:
		return team.ReportKindSynthesis, true
	default:
		return "", false
	}
}

func outputContractForTask(task team.Task) string {
	if _, ok := reportKindForTask(task); ok {
		return "typed_report"
	}
	return "display"
}

func responseFormatForTask(task team.Task) *provider.ResponseFormat {
	kind, ok := reportKindForTask(task)
	if !ok {
		return nil
	}
	return &provider.ResponseFormat{
		Type:   "json_schema",
		Name:   string(kind) + "_report",
		Strict: true,
		Schema: schemaPointer(reportSchemaEnvelope(kind)),
	}
}

func typedReportGuardrailForTask(task team.Task) agent.OutputGuardrail {
	kind, ok := reportKindForTask(task)
	if !ok {
		return nil
	}
	return agent.NewOutputGuardrail("typed_report_required", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
		if validTypedReport(kind, input.Output.Text) {
			return agent.AllowOutput(), nil
		}
		return agent.RetryOutput(message.NewText(message.RoleUser, typedReportRetryPrompt(kind))), nil
	})
}

func validTypedReport(kind team.ReportKind, text string) bool {
	structured := parseStructuredText(text)
	if len(structured) == 0 {
		return false
	}
	switch kind {
	case team.ReportKindResearch:
		report, ok := team.ExtractResearchReport(structured)
		return ok && team.ValidateResearchReport(report) == nil
	case team.ReportKindVerification:
		report, ok := team.ExtractVerificationReport(structured)
		return ok && len(report.PerClaim) > 0 && team.ValidateVerificationReport(report) == nil
	case team.ReportKindSynthesis:
		report, ok := team.ExtractSynthesisReport(structured)
		return ok && team.ValidateSynthesisReport(report) == nil
	default:
		return false
	}
}

func typedReportRetryPrompt(kind team.ReportKind) string {
	switch kind {
	case team.ReportKindResearch:
		return `Your previous output was not a valid research report. Return exactly one JSON object with top-level field "report". The report must have kind="research" and include at least one claim. Findings are optional, but findings-only reports are not valid because panel verification is claim-based. Do not emit prose, markdown fences, or commentary outside the JSON object.`
	case team.ReportKindVerification:
		return `Your previous output was not a valid verification report. Return exactly one JSON object with top-level field "report". The report must have kind="verification", a status of "supported", "contradicted", or "insufficient", and explicit perClaim statuses when adjudicating claims. Do not emit prose, markdown fences, or commentary outside the JSON object.`
	case team.ReportKindSynthesis:
		return `Your previous output was not a valid synthesis report. Return exactly one JSON object with top-level field "report". The report must have kind="synthesis" and a non-empty answer field. Do not emit prose, markdown fences, or commentary outside the JSON object.`
	default:
		return `Your previous output was not a valid typed report. Return exactly one JSON object with top-level field "report".`
	}
}

func reportSchemaForKind(kind team.ReportKind) message.JSONSchema {
	switch kind {
	case team.ReportKindResearch:
		return researchReportSchema()
	case team.ReportKindVerification:
		return verificationReportSchema()
	case team.ReportKindSynthesis:
		return synthesisReportSchema()
	default:
		return message.JSONSchema{Type: "object"}
	}
}

func researchReportSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "object",
		Properties: map[string]message.JSONSchema{
			"kind":       {Type: "string", Enum: []string{string(team.ReportKindResearch)}},
			"claims":     reportClaimsSchema(),
			"evidence":   reportEvidenceSchema(),
			"findings":   reportFindingsSchema(),
			"confidence": {Type: "number"},
			"notes":      {Type: "string"},
			"metadata":   {Type: "object", AdditionalProperties: true},
		},
		Required: []string{"kind", "claims"},
	}
}

func reportClaimsSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "array",
		Items: schemaPointer(message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"id":          {Type: "string"},
				"summary":     {Type: "string"},
				"evidenceIds": {Type: "array", Items: &message.JSONSchema{Type: "string"}},
				"confidence":  {Type: "number"},
			},
			Required: []string{"summary"},
		}),
	}
}

func reportEvidenceSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "array",
		Items: schemaPointer(message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"id":      {Type: "string"},
				"source":  {Type: "string"},
				"snippet": {Type: "string"},
				"url":     {Type: "string"},
				"score":   {Type: "number"},
			},
			Required: []string{"snippet"},
		}),
	}
}

func reportFindingsSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "array",
		Items: schemaPointer(message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"id":         {Type: "string"},
				"summary":    {Type: "string"},
				"claimIds":   {Type: "array", Items: &message.JSONSchema{Type: "string"}},
				"confidence": {Type: "number"},
			},
			Required: []string{"summary"},
		}),
	}
}

func verificationReportSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "object",
		Properties: map[string]message.JSONSchema{
			"kind":       {Type: "string", Enum: []string{string(team.ReportKindVerification)}},
			"status":     {Type: "string", Enum: verificationStatusEnums()},
			"confidence": {Type: "number"},
			"perClaim":   perClaimSchema(),
			"reason":     {Type: "string"},
			"metadata":   {Type: "object", AdditionalProperties: true},
		},
		Required: []string{"kind", "status", "perClaim"},
	}
}

func perClaimSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "array",
		Items: schemaPointer(message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"claimId":     {Type: "string"},
				"status":      {Type: "string", Enum: verificationStatusEnums()},
				"confidence":  {Type: "number"},
				"evidenceIds": {Type: "array", Items: &message.JSONSchema{Type: "string"}},
				"reason":      {Type: "string"},
			},
			Required: []string{"claimId", "status"},
		}),
	}
}

func synthesisReportSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "object",
		Properties: map[string]message.JSONSchema{
			"kind":      {Type: "string", Enum: []string{string(team.ReportKindSynthesis)}},
			"answer":    {Type: "string"},
			"citations": citationSchema(),
		},
		Required: []string{"kind", "answer"},
	}
}

func citationSchema() message.JSONSchema {
	return message.JSONSchema{
		Type: "array",
		Items: schemaPointer(message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"exchangeId": {Type: "string"},
				"findingId":  {Type: "string"},
				"claimId":    {Type: "string"},
				"excerpt":    {Type: "string"},
			},
		}),
	}
}

func verificationStatusEnums() []string {
	return []string{
		string(team.VerificationStatusSupported),
		string(team.VerificationStatusContradicted),
		string(team.VerificationStatusInsufficient),
	}
}

func reportSchemaEnvelope(kind team.ReportKind) message.JSONSchema {
	return message.JSONSchema{
		Type: "object",
		Properties: map[string]message.JSONSchema{
			team.ReportKey: reportSchemaForKind(kind),
		},
		Required: []string{team.ReportKey},
	}
}

func schemaPointer(schema message.JSONSchema) *message.JSONSchema {
	return &schema
}
