package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/observe"
)

type ArtifactInfo struct {
	ID             string
	Kind           evaluation.ArtifactManifestKind
	Path           string
	Content        string
	needsRedaction bool
	redacted       bool
}

func GenerateManifest(runID string, artifacts []ArtifactInfo) *evaluation.ArtifactManifest {
	entries := make([]evaluation.ArtifactManifestEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		content := artifact.Content
		redacted := artifact.redacted
		if artifact.needsRedaction {
			var changed bool
			content, changed = observe.RedactSecrets(content)
			redacted = redacted || changed
		}
		entries = append(entries, evaluation.ArtifactManifestEntry{
			ID:             artifact.ID,
			Kind:           artifact.Kind,
			Path:           stableArtifactPath(runID, artifact.Kind, artifact.Path),
			URI:            "file://" + stableArtifactPath(runID, artifact.Kind, artifact.Path),
			Checksum:       checksumForContent(content),
			Size:           int64(len(content)),
			ContentType:    contentTypeForPath(artifact.Path),
			RetentionClass: retentionClassForKind(artifact.Kind),
			Redacted:       redacted,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind == entries[j].Kind {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Kind < entries[j].Kind
	})
	return &evaluation.ArtifactManifest{
		SchemaVersion: evaluation.ArtifactManifestSchemaVersion,
		RunID:         runID,
		CreatedAt:     time.Now().UTC(),
		Entries:       entries,
	}
}

func persistArtifactManifest(result RunResult) error {
	runID := filepath.Base(result.OutputDir)
	artifacts, err := persistManifestArtifacts(result, runID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(result.OutputDir, "manifest.json"), GenerateManifest(runID, artifacts))
}

func persistManifestArtifacts(result RunResult, runID string) ([]ArtifactInfo, error) {
	artifacts := make([]ArtifactInfo, 0, 8)

	evaluationReportPath := filepath.Join(result.OutputDir, "evaluation_report.json")
	evaluationReportContent, err := marshalArtifactContent(result.Comparison)
	if err != nil {
		return nil, err
	}
	artifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "evaluation-report",
		kind:           evaluation.ArtifactManifestKindEvaluationReport,
		path:           evaluationReportPath,
		content:        evaluationReportContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, artifact)

	summaryPath := filepath.Join(result.OutputDir, "summary.md")
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "summary",
		kind:           evaluation.ArtifactManifestKindSummary,
		path:           summaryPath,
		content:        RenderComparisonMarkdown(result.Comparison),
		needsRedaction: true,
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, artifact)

	scorePath := filepath.Join(result.OutputDir, "score.json")
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "score",
		kind:           evaluation.ArtifactManifestKindScore,
		path:           scorePath,
		content:        buildScoreArtifactContent(runID, result),
		needsRedaction: true,
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, artifact)

	toolCallsContent, err := aggregateCommandLogs(result.CommandResults, true)
	if err != nil {
		return nil, err
	}
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "tool-calls",
		kind:           evaluation.ArtifactManifestKindToolCalls,
		path:           filepath.Join(result.OutputDir, "tool_calls.log"),
		content:        toolCallsContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, artifact)

	modelEventsContent, err := aggregateCommandLogs(result.CommandResults, false)
	if err != nil {
		return nil, err
	}
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "model-events",
		kind:           evaluation.ArtifactManifestKindModelEvents,
		path:           filepath.Join(result.OutputDir, "model_events.log"),
		content:        modelEventsContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, artifact)

	traceArtifacts, err := persistTraceArtifacts(result)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, traceArtifacts...)

	return artifacts, nil
}

type artifactWriteRequest struct {
	id             string
	kind           evaluation.ArtifactManifestKind
	path           string
	content        string
	needsRedaction bool
}

func writeArtifactFile(request artifactWriteRequest) (ArtifactInfo, error) {
	content := request.content
	redacted := false
	if request.needsRedaction {
		var changed bool
		content, changed = observe.RedactSecrets(content)
		redacted = changed
	}
	if err := os.WriteFile(request.path, []byte(content), 0o644); err != nil {
		return ArtifactInfo{}, err
	}
	return ArtifactInfo{
		ID:             request.id,
		Kind:           request.kind,
		Path:           request.path,
		Content:        content,
		needsRedaction: request.needsRedaction,
		redacted:       redacted,
	}, nil
}

func persistTraceArtifacts(result RunResult) ([]ArtifactInfo, error) {
	artifacts := []ArtifactInfo{}
	for _, candidate := range []struct {
		id   string
		kind evaluation.ArtifactManifestKind
		path string
	}{
		{id: "events", kind: evaluation.ArtifactManifestKindEvents, path: result.Trace.EventsPath},
		{id: "replay-state", kind: evaluation.ArtifactManifestKindReplayState, path: result.Trace.ReplayStatePath},
	} {
		if candidate.path == "" {
			continue
		}
		content, err := os.ReadFile(candidate.path)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(result.OutputDir, string(candidate.kind)+filepath.Ext(candidate.path))
		artifact, err := writeArtifactFile(artifactWriteRequest{
			id:             candidate.id,
			kind:           candidate.kind,
			path:           targetPath,
			content:        string(content),
			needsRedaction: true,
		})
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	if result.Trace.EvaluationPath != "" {
		content, err := os.ReadFile(result.Trace.EvaluationPath)
		if err != nil {
			return nil, err
		}
		artifact, err := writeArtifactFile(artifactWriteRequest{
			id:             "evaluation-report-trace",
			kind:           evaluation.ArtifactManifestKindEvaluationReport,
			path:           filepath.Join(result.OutputDir, string(evaluation.ArtifactManifestKindEvaluationReport)+filepath.Ext(result.Trace.EvaluationPath)),
			content:        string(content),
			needsRedaction: true,
		})
		if err != nil {
			return nil, err
		}
		for i := range artifacts {
			if artifacts[i].Kind == evaluation.ArtifactManifestKindEvaluationReport {
				artifacts[i] = artifact
				return artifacts, nil
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func marshalArtifactContent(value any) (string, error) {
	data, err := MarshalIndented(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func aggregateCommandLogs(results []CommandResult, stdout bool) (string, error) {
	sections := make([]string, 0, len(results))
	for _, result := range results {
		path := result.StderrPath
		stream := "stderr"
		if stdout {
			path = result.StdoutPath
			stream = "stdout"
		}
		if path == "" {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sections = append(sections, fmt.Sprintf("== %s %s ==\n%s", result.Phase, stream, string(content)))
	}
	return strings.Join(sections, "\n"), nil
}

func buildScoreArtifactContent(runID string, result RunResult) string {
	payload := struct {
		SchemaVersion     string             `json:"schemaVersion"`
		RunID             string             `json:"runId"`
		Scores            map[string]float64 `json:"scores,omitempty"`
		OfficialScoreFile string             `json:"officialScoreFile,omitempty"`
		Cost              CostInfo           `json:"cost"`
	}{
		SchemaVersion:     evaluation.ScorePayloadSchemaVersion,
		RunID:             runID,
		Scores:            result.Scores,
		OfficialScoreFile: result.OfficialScoreFile,
		Cost:              result.Cost,
	}
	content, err := marshalArtifactContent(payload)
	if err != nil {
		return "{}"
	}
	return content
}

func stableArtifactPath(runID string, kind evaluation.ArtifactManifestKind, artifactPath string) string {
	ext := strings.TrimPrefix(filepath.Ext(artifactPath), ".")
	if ext == "" {
		ext = defaultArtifactExtension(kind)
	}
	return filepath.ToSlash(filepath.Join("runs", runID, string(kind)+"."+ext))
}

func defaultArtifactExtension(kind evaluation.ArtifactManifestKind) string {
	switch kind {
	case evaluation.ArtifactManifestKindSummary:
		return "md"
	case evaluation.ArtifactManifestKindEvents:
		return "ndjson"
	case evaluation.ArtifactManifestKindToolCalls, evaluation.ArtifactManifestKindModelEvents:
		return "log"
	default:
		return "json"
	}
}

func retentionClassForKind(kind evaluation.ArtifactManifestKind) evaluation.RetentionClass {
	switch kind {
	case evaluation.ArtifactManifestKindEvents, evaluation.ArtifactManifestKindReplayState:
		return evaluation.RetentionClassLongTerm
	case evaluation.ArtifactManifestKindScore, evaluation.ArtifactManifestKindSummary:
		return evaluation.RetentionClassPermanent
	case evaluation.ArtifactManifestKindToolCalls, evaluation.ArtifactManifestKindModelEvents:
		return evaluation.RetentionClassShortTerm
	default:
		return evaluation.RetentionClassLongTerm
	}
}

func contentTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "application/json"
	case ".jsonl", ".ndjson":
		return "application/x-ndjson"
	case ".md":
		return "text/markdown"
	case ".log", ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func checksumForContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}
