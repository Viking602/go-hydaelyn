package evaluation

import (
	"fmt"
	"strings"
	"time"
)

// SummaryReport is a human-readable evaluation summary
type SummaryReport struct {
	RunID            string
	CaseID           string
	Mode             EvalRunMode
	StartedAt        time.Time
	CompletedAt      time.Time
	Duration         time.Duration
	Level            ScoreLevel
	OverallScore     float64
	ReplayConsistent bool
	ReleaseDecision  ReleaseDecision
	RuntimeMetrics   *ScoreRuntimeMetrics
	QualityMetrics   *ScoreQualityMetrics
	SafetyMetrics    *ScoreSafetyMetrics
	Failures         []ScoreFailure
	Recommendations  []ScoreRecommendation
}

// GenerateSummaryReport creates a human-readable summary report from an eval run and score
func GenerateSummaryReport(run *EvalRun, score *ScorePayload) string {
	if run == nil {
		return "Error: nil eval run"
	}

	var sb strings.Builder

	// Header
	sb.WriteString("╔════════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║           EVALUATION SUMMARY REPORT                            ║\n")
	sb.WriteString("╚════════════════════════════════════════════════════════════════╝\n\n")

	// Run Information
	sb.WriteString("RUN INFORMATION\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	fmt.Fprintf(&sb, "  Run ID:          %s\n", run.ID)
	fmt.Fprintf(&sb, "  Case ID:         %s\n", run.CaseID)
	fmt.Fprintf(&sb, "  Mode:            %s\n", run.Mode)
	fmt.Fprintf(&sb, "  Status:          %s\n", run.Status)
	if !run.StartedAt.IsZero() {
		fmt.Fprintf(&sb, "  Started:         %s\n", run.StartedAt.Format(time.RFC3339))
	}
	if !run.CompletedAt.IsZero() {
		fmt.Fprintf(&sb, "  Completed:       %s\n", run.CompletedAt.Format(time.RFC3339))
		duration := run.CompletedAt.Sub(run.StartedAt)
		fmt.Fprintf(&sb, "  Duration:        %s\n", duration.Round(time.Millisecond))
	}
	sb.WriteString("\n")

	// Score Summary (if available)
	if score != nil {
		sb.WriteString("SCORE SUMMARY\n")
		sb.WriteString(strings.Repeat("─", 60) + "\n")
		fmt.Fprintf(&sb, "  Overall Score:   %.3f\n", score.OverallScore)
		fmt.Fprintf(&sb, "  Level:           %s\n", score.Level)
		fmt.Fprintf(&sb, "  Replay Status:   %s\n", replayStatus(score.ReplayConsistent))

		// Release Decision
		decision := EvaluateReleaseGate(score)
		fmt.Fprintf(&sb, "  Release Gate:    %s\n", formatDecision(decision))
		sb.WriteString("\n")

		// Runtime Metrics
		if score.RuntimeMetrics != nil {
			sb.WriteString("RUNTIME METRICS\n")
			sb.WriteString(strings.Repeat("─", 60) + "\n")
			m := score.RuntimeMetrics
			fmt.Fprintf(&sb, "  Task Completion Rate:    %.1f%%\n", m.TaskCompletionRate*100)
			fmt.Fprintf(&sb, "  Blocking Failure Rate:   %.1f%%\n", m.BlockingFailureRate*100)
			fmt.Fprintf(&sb, "  Retry Success Rate:      %.1f%%\n", m.RetrySuccessRate*100)
			fmt.Fprintf(&sb, "  Token Budget Hit Rate:   %.1f%%\n", m.TokenBudgetHitRate*100)
			if m.EndToEndLatencyMs > 0 {
				fmt.Fprintf(&sb, "  End-to-End Latency:      %d ms\n", m.EndToEndLatencyMs)
			}
			if m.ToolCallCount > 0 {
				fmt.Fprintf(&sb, "  Tool Call Count:         %d\n", m.ToolCallCount)
			}
			sb.WriteString("\n")
		}

		// Quality Metrics
		if score.QualityMetrics != nil {
			sb.WriteString("QUALITY METRICS\n")
			sb.WriteString(strings.Repeat("─", 60) + "\n")
			m := score.QualityMetrics
			fmt.Fprintf(&sb, "  Answer Correctness:      %.1f%%\n", m.AnswerCorrectness*100)
			fmt.Fprintf(&sb, "  Groundedness:            %.1f%%\n", m.Groundedness*100)
			fmt.Fprintf(&sb, "  Citation Precision:      %.1f%%\n", m.CitationPrecision*100)
			fmt.Fprintf(&sb, "  Citation Recall:         %.1f%%\n", m.CitationRecall*100)
			fmt.Fprintf(&sb, "  Tool Precision:          %.1f%%\n", m.ToolPrecision*100)
			fmt.Fprintf(&sb, "  Tool Recall:             %.1f%%\n", m.ToolRecall*100)
			fmt.Fprintf(&sb, "  Tool Arg Accuracy:       %.1f%%\n", m.ToolArgAccuracy*100)
			fmt.Fprintf(&sb, "  Synthesis Input Coverage: %.1f%%\n", m.SynthesisInputCoverage*100)
			sb.WriteString("\n")
		}

		// Safety Metrics
		if score.SafetyMetrics != nil {
			sb.WriteString("SAFETY METRICS\n")
			sb.WriteString(strings.Repeat("─", 60) + "\n")
			m := score.SafetyMetrics
			fmt.Fprintf(&sb, "  Critical Failure:        %s\n", boolStatus(m.CriticalFailure))
			fmt.Fprintf(&sb, "  Prompt Injection Blocked: %s\n", boolStatus(m.PromptInjectionBlocked))
			fmt.Fprintf(&sb, "  Unauthorized Tool Blocked: %s\n", boolStatus(m.UnauthorizedToolBlocked))
			fmt.Fprintf(&sb, "  Secret Leak Blocked:     %s\n", boolStatus(m.SecretLeakBlocked))
			sb.WriteString("\n")
		}

		// Failures
		if len(score.Failures) > 0 {
			sb.WriteString("FAILURES\n")
			sb.WriteString(strings.Repeat("─", 60) + "\n")
			for i, f := range score.Failures {
				fmt.Fprintf(&sb, "  %d. [%s] %s\n", i+1, f.Severity, f.Code)
				fmt.Fprintf(&sb, "     Metric: %s | Layer: %s\n", f.Metric, f.Layer)
				if f.Message != "" {
					fmt.Fprintf(&sb, "     %s\n", f.Message)
				}
				if f.Blocking {
					sb.WriteString("     [BLOCKING]\n")
				}
				sb.WriteString("\n")
			}
		}

		// Recommendations
		if len(score.Recommendations) > 0 {
			sb.WriteString("RECOMMENDATIONS\n")
			sb.WriteString(strings.Repeat("─", 60) + "\n")
			for i, r := range score.Recommendations {
				fmt.Fprintf(&sb, "  %d. [%s] %s\n", i+1, r.Priority, r.Action)
				if r.Rationale != "" {
					fmt.Fprintf(&sb, "     %s\n", r.Rationale)
				}
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("SCORE: Not available\n\n")
	}

	// Provenance (if live mode)
	if run.Provenance != nil {
		sb.WriteString("PROVENANCE\n")
		sb.WriteString(strings.Repeat("─", 60) + "\n")
		fmt.Fprintf(&sb, "  Lane:     %s\n", run.Provenance.LaneID)
		fmt.Fprintf(&sb, "  Provider: %s\n", run.Provenance.Provider)
		fmt.Fprintf(&sb, "  Model:    %s\n", run.Provenance.Model)
		sb.WriteString("\n")
	}

	// Usage
	if run.Usage != nil {
		sb.WriteString("USAGE\n")
		sb.WriteString(strings.Repeat("─", 60) + "\n")
		fmt.Fprintf(&sb, "  Prompt Tokens:     %d\n", run.Usage.PromptTokens)
		fmt.Fprintf(&sb, "  Completion Tokens: %d\n", run.Usage.CompletionTokens)
		fmt.Fprintf(&sb, "  Total Tokens:      %d\n", run.Usage.TotalTokens)
		if run.Usage.TotalCostUSD > 0 {
			fmt.Fprintf(&sb, "  Total Cost:        $%.4f\n", run.Usage.TotalCostUSD)
		}
		if run.Usage.LatencyMs > 0 {
			fmt.Fprintf(&sb, "  Latency:           %d ms\n", run.Usage.LatencyMs)
		}
		if run.Usage.ToolCallCount > 0 {
			fmt.Fprintf(&sb, "  Tool Calls:        %d\n", run.Usage.ToolCallCount)
		}
		sb.WriteString("\n")
	}

	// Variance
	if run.Variance != nil {
		sb.WriteString("VARIANCE\n")
		sb.WriteString(strings.Repeat("─", 60) + "\n")
		fmt.Fprintf(&sb, "  Compared to:       %s\n", run.Variance.ComparedRunID)
		fmt.Fprintf(&sb, "  Window:            %d runs\n", run.Variance.Window)
		fmt.Fprintf(&sb, "  Within Tolerance:  %s\n", boolStatus(run.Variance.WithinTolerance))
		if len(run.Variance.ExceededMetrics) > 0 {
			sb.WriteString("  Exceeded Metrics:  " + strings.Join(run.Variance.ExceededMetrics, ", ") + "\n")
		}
		sb.WriteString("\n")
	}

	// Footer
	sb.WriteString(strings.Repeat("═", 60) + "\n")
	sb.WriteString("Generated: " + time.Now().UTC().Format(time.RFC3339) + "\n")

	return sb.String()
}

// GenerateSummaryReportFromPayload creates a summary directly from score payload
func GenerateSummaryReportFromPayload(score *ScorePayload) string {
	if score == nil {
		return "Error: nil score payload"
	}

	// Create a minimal EvalRun for the report
	run := &EvalRun{
		SchemaVersion: EvalRunSchemaVersion,
		ID:            score.RunID,
		Status:        EvalRunStatusCompleted,
	}

	return GenerateSummaryReport(run, score)
}

// Helper functions

func replayStatus(consistent bool) string {
	if consistent {
		return "✓ Consistent"
	}
	return "✗ Inconsistent"
}

func boolStatus(value bool) string {
	if value {
		return "✓ Yes"
	}
	return "✗ No"
}

func formatDecision(decision ReleaseDecision) string {
	switch decision {
	case ReleaseDecisionGo:
		return "✓ GO - Ready for release"
	case ReleaseDecisionConditional:
		return "⚠ CONDITIONAL GO - Review required"
	case ReleaseDecisionNoGo:
		return "✗ NO GO - Blocked"
	default:
		return "? Unknown"
	}
}

// LevelDescription returns a human-readable description of a score level
func LevelDescription(level ScoreLevel) string {
	switch level {
	case ScoreLevelA0:
		return "A0: Fundamental runtime failures - not functional"
	case ScoreLevelA1:
		return "A1: Critical quality or safety failures"
	case ScoreLevelA2:
		return "A2: Acceptable with conditions - minor issues"
	case ScoreLevelA3:
		return "A3: Good quality - meets standards"
	case ScoreLevelA4:
		return "A4: Excellent quality - exceeds expectations"
	default:
		return "Unknown level"
	}
}

// DecisionDescription returns a human-readable description of a release decision
func DecisionDescription(decision ReleaseDecision) string {
	switch decision {
	case ReleaseDecisionGo:
		return "Go: Release approved - all criteria met"
	case ReleaseDecisionConditional:
		return "Conditional Go: Release requires approval - known issues"
	case ReleaseDecisionNoGo:
		return "No Go: Release blocked - critical issues"
	default:
		return "Unknown decision"
	}
}
