package benchmark

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/providers/anthropic"
	"github.com/Viking602/go-hydaelyn/providers/openai"
	"github.com/Viking602/go-hydaelyn/session"
)

const (
	longMemEvalOracleFile  = "longmemeval_oracle.json"
	longMemEvalSmokeSample = 5
)

type longMemEvalEntry struct {
	QuestionID       string              `json:"question_id"`
	QuestionType     string              `json:"question_type"`
	Question         string              `json:"question"`
	Answer           any                 `json:"answer"`
	QuestionDate     string              `json:"question_date"`
	HaystackDates    []string            `json:"haystack_dates"`
	HaystackSessions [][]longMemEvalTurn `json:"haystack_sessions"`
}

type longMemEvalTurn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer bool   `json:"has_answer,omitempty"`
}

type longMemEvalHypothesis struct {
	QuestionID string `json:"question_id"`
	Hypothesis string `json:"hypothesis"`
}

type longMemEvalEvalLog struct {
	QuestionID    string `json:"question_id"`
	AutoEvalLabel struct {
		Model string `json:"model"`
		Label bool   `json:"label"`
	} `json:"autoeval_label"`
}

func prepareBenchmarkArtifacts(ctx context.Context, bench BenchmarkSpec, lane LaneSpec, benchmarkDir, outputDir, mode string) (ScoreBundle, error) {
	switch bench.ID {
	case "longmemeval":
		return prepareLongMemEvalArtifacts(ctx, lane, benchmarkDir, outputDir, mode)
	default:
		return ScoreBundle{Scores: map[string]float64{}}, nil
	}
}

func collectBenchmarkResults(bench BenchmarkSpec, lane LaneSpec, outputDir string) (ScoreBundle, error) {
	switch bench.ID {
	case "longmemeval":
		return collectLongMemEvalResults(outputDir, lane.JudgeModel)
	default:
		return ScoreBundle{Scores: map[string]float64{}}, nil
	}
}

func prepareLongMemEvalArtifacts(ctx context.Context, lane LaneSpec, benchmarkDir, outputDir, mode string) (ScoreBundle, error) {
	dataPath, err := ensureLongMemEvalOracleData(benchmarkDir)
	if err != nil {
		return ScoreBundle{}, err
	}
	entries, err := loadLongMemEvalEntries(dataPath)
	if err != nil {
		return ScoreBundle{}, err
	}
	if mode == "smoke" && len(entries) > longMemEvalSmokeSample {
		entries = entries[:longMemEvalSmokeSample]
	}
	referencePath := filepath.Join(outputDir, "reference.json")
	if err := writeJSON(referencePath, entries); err != nil {
		return ScoreBundle{}, err
	}
	hypothesesPath := filepath.Join(outputDir, "hypotheses.jsonl")
	if info, err := os.Stat(hypothesesPath); err == nil && info.Size() > 0 {
		return ScoreBundle{Scores: map[string]float64{}}, nil
	}
	runner, providerName, err := newLongMemEvalRunner(lane)
	if err != nil {
		return ScoreBundle{}, err
	}
	hypothesesFile, err := os.Create(hypothesesPath)
	if err != nil {
		return ScoreBundle{}, err
	}
	defer hypothesesFile.Close()
	writer := bufio.NewWriter(hypothesesFile)
	cost := CostInfo{}
	startedAt := time.Now()
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ScoreBundle{}, ctx.Err()
		default:
		}
		hypothesis, usage, err := runLongMemEvalQuestion(ctx, runner, providerName, lane.Model, entry)
		if err != nil {
			return ScoreBundle{}, err
		}
		item := longMemEvalHypothesis{
			QuestionID: entry.QuestionID,
			Hypothesis: hypothesis,
		}
		payload, err := json.Marshal(item)
		if err != nil {
			return ScoreBundle{}, err
		}
		if _, err := writer.Write(payload); err != nil {
			return ScoreBundle{}, err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return ScoreBundle{}, err
		}
		cost.PromptTokens += usage.InputTokens
		cost.CompletionTokens += usage.OutputTokens
		cost.TotalTokens += usage.TotalTokens
	}
	if err := writer.Flush(); err != nil {
		return ScoreBundle{}, err
	}
	cost.LatencyMs = time.Since(startedAt).Milliseconds()
	return ScoreBundle{
		Scores: map[string]float64{},
		Cost:   cost,
	}, nil
}

func collectLongMemEvalResults(outputDir, judgeModel string) (ScoreBundle, error) {
	if judgeModel == "" {
		judgeModel = "gpt-4o"
	}
	logPath := filepath.Join(outputDir, "hypotheses.jsonl.eval-results-"+judgeModel)
	referencePath := filepath.Join(outputDir, "reference.json")
	entries, err := loadLongMemEvalEntries(referencePath)
	if err != nil {
		return ScoreBundle{}, err
	}
	logs, err := loadLongMemEvalEvalLogs(logPath)
	if err != nil {
		return ScoreBundle{}, err
	}
	references := make(map[string]longMemEvalEntry, len(entries))
	for _, entry := range entries {
		references[entry.QuestionID] = entry
	}
	scores := map[string]float64{}
	perType := map[string][]float64{}
	all := make([]float64, 0, len(logs))
	abstention := []float64{}
	for _, item := range logs {
		entry, ok := references[item.QuestionID]
		if !ok {
			continue
		}
		value := 0.0
		if item.AutoEvalLabel.Label {
			value = 1.0
		}
		all = append(all, value)
		perType[entry.QuestionType] = append(perType[entry.QuestionType], value)
		if strings.HasSuffix(item.QuestionID, "_abs") {
			abstention = append(abstention, value)
		}
	}
	if len(all) > 0 {
		scores["qaAccuracy"] = average(all)
	}
	if len(abstention) > 0 {
		scores["abstentionAccuracy"] = average(abstention)
	}
	typeKeys := make([]string, 0, len(perType))
	for key := range perType {
		typeKeys = append(typeKeys, key)
	}
	sort.Strings(typeKeys)
	for _, key := range typeKeys {
		scores["questionType."+key] = average(perType[key])
	}
	return ScoreBundle{
		Scores:            scores,
		OfficialScoreFile: logPath,
	}, nil
}

func ensureLongMemEvalOracleData(benchmarkDir string) (string, error) {
	dataDir := filepath.Join(benchmarkDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	dataPath := filepath.Join(dataDir, longMemEvalOracleFile)
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath, nil
	}
	url := "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/" + longMemEvalOracleFile
	response, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("download %s: %s %s", longMemEvalOracleFile, response.Status, strings.TrimSpace(string(payload)))
	}
	file, err := os.Create(dataPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return "", err
	}
	return dataPath, nil
}

func loadLongMemEvalEntries(path string) ([]longMemEvalEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []longMemEvalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode longmemeval entries: %w", err)
	}
	return entries, nil
}

func loadLongMemEvalEvalLogs(path string) ([]longMemEvalEvalLog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	items := []longMemEvalEvalLog{}
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 1024*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	for scanner.Scan() {
		var item longMemEvalEvalLog
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("decode longmemeval eval log: %w", err)
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func newLongMemEvalRunner(lane LaneSpec) (*host.Runtime, string, error) {
	runner := host.New(host.Config{})
	providerName := "bench"
	baseURL := laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv)
	switch lane.Provider {
	case "openai", "openrouter":
		runner.RegisterProvider(providerName, openai.New(openai.Config{
			APIKey:  os.Getenv(lane.APIKeyEnv),
			BaseURL: baseURL,
			Models:  []string{lane.Model},
		}))
	case "anthropic":
		runner.RegisterProvider(providerName, anthropic.New(anthropic.Config{
			APIKey:  os.Getenv(lane.APIKeyEnv),
			BaseURL: baseURL,
			Models:  []string{lane.Model},
		}))
	default:
		return nil, "", fmt.Errorf("longmemeval does not support provider %q", lane.Provider)
	}
	return runner, providerName, nil
}

func runLongMemEvalQuestion(ctx context.Context, runner *host.Runtime, providerName, model string, entry longMemEvalEntry) (string, hostPromptUsage, error) {
	sess, err := runner.CreateSession(ctx, session.CreateParams{Branch: "longmemeval"})
	if err != nil {
		return "", hostPromptUsage{}, err
	}
	response, err := runner.Prompt(ctx, host.PromptRequest{
		SessionID: sess.ID,
		Provider:  providerName,
		Model:     model,
		Messages:  []message.Message{message.NewText(message.RoleUser, buildLongMemEvalPrompt(entry))},
	})
	if err != nil {
		return "", hostPromptUsage{}, err
	}
	answer := extractAssistantText(response.Messages)
	if answer == "" {
		return "", hostPromptUsage{}, fmt.Errorf("longmemeval prompt for %s returned no assistant text", entry.QuestionID)
	}
	return answer, hostPromptUsage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}, nil
}

type hostPromptUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func buildLongMemEvalPrompt(entry longMemEvalEntry) string {
	type sessionChunk struct {
		Date    string
		Session []longMemEvalTurn
	}
	chunks := make([]sessionChunk, 0, len(entry.HaystackSessions))
	for index, sessionEntry := range entry.HaystackSessions {
		date := ""
		if index < len(entry.HaystackDates) {
			date = entry.HaystackDates[index]
		}
		cleaned := make([]longMemEvalTurn, 0, len(sessionEntry))
		for _, turn := range sessionEntry {
			cleaned = append(cleaned, longMemEvalTurn{
				Role:    turn.Role,
				Content: strings.TrimSpace(turn.Content),
			})
		}
		chunks = append(chunks, sessionChunk{Date: date, Session: cleaned})
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].Date < chunks[j].Date
	})
	var history strings.Builder
	for index, chunk := range chunks {
		payload, _ := json.Marshal(chunk.Session)
		history.WriteString(fmt.Sprintf("\n### Session %d:\nSession Date: %s\nSession Content:\n%s\n", index+1, chunk.Date, payload))
	}
	return fmt.Sprintf(
		"I will give you several history chats between you and a user. Please answer the question based on the relevant chat history.\n\n\nHistory Chats:\n%s\nCurrent Date: %s\nQuestion: %s\nAnswer:",
		history.String(),
		entry.QuestionDate,
		entry.Question,
	)
}

func extractAssistantText(messages []message.Message) string {
	parts := []string{}
	for _, msg := range messages {
		if msg.Role != message.RoleAssistant {
			continue
		}
		text := strings.TrimSpace(msg.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
