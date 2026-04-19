package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

const DefaultCatalogPath = "benchmarks/catalog.json"

type Catalog struct {
	Version          string          `json:"version"`
	DefaultOutputDir string          `json:"defaultOutputDir,omitempty"`
	Benchmarks       []BenchmarkSpec `json:"benchmarks"`
	Lanes            []LaneSpec      `json:"lanes"`
}

type BenchmarkSpec struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Tier               string             `json:"tier,omitempty"`
	Family             string             `json:"family,omitempty"`
	Summary            string             `json:"summary,omitempty"`
	OfficialRepoURL    string             `json:"officialRepoUrl,omitempty"`
	OfficialWebsiteURL string             `json:"officialWebsiteUrl,omitempty"`
	OfficialPaperURL   string             `json:"officialPaperUrl,omitempty"`
	OfficialDataURL    string             `json:"officialDataUrl,omitempty"`
	OfficialRef        string             `json:"officialRef,omitempty"`
	PrimaryMetrics     []string           `json:"primaryMetrics,omitempty"`
	SetupCommands      []string           `json:"setupCommands,omitempty"`
	SmokeCommands      []string           `json:"smokeCommands,omitempty"`
	NightlyCommands    []string           `json:"nightlyCommands,omitempty"`
	Baselines          []BaselineSnapshot `json:"baselines,omitempty"`
}

type BaselineSnapshot struct {
	Label      string             `json:"label"`
	SourceURL  string             `json:"sourceUrl,omitempty"`
	CapturedAt string             `json:"capturedAt,omitempty"`
	Notes      string             `json:"notes,omitempty"`
	Default    bool               `json:"default,omitempty"`
	Scores     map[string]float64 `json:"scores,omitempty"`
}

type LaneSpec struct {
	ID                     string             `json:"id"`
	Provider               string             `json:"provider"`
	Model                  string             `json:"model"`
	ProviderProvenance     string             `json:"providerProvenance,omitempty"`
	ModelProvenance        string             `json:"modelProvenance,omitempty"`
	APIKeyEnv              string             `json:"apiKeyEnv,omitempty"`
	BaseURL                string             `json:"baseUrl,omitempty"`
	BaseURLEnv             string             `json:"baseUrlEnv,omitempty"`
	PromptCostPer1KUSD     float64            `json:"promptCostPer1kUsd,omitempty"`
	CompletionCostPer1KUSD float64            `json:"completionCostPer1kUsd,omitempty"`
	MaxTokens              int                `json:"maxTokens,omitempty"`
	MaxToolCalls           int                `json:"maxToolCalls,omitempty"`
	MaxCostUSD             float64            `json:"maxCostUsd,omitempty"`
	MetricTolerances       map[string]float64 `json:"metricTolerances,omitempty"`
	LatencyToleranceMs     int64              `json:"latencyToleranceMs,omitempty"`
	CostToleranceUSD       float64            `json:"costToleranceUsd,omitempty"`
	RequiredSecrets        []string           `json:"requiredSecrets,omitempty"`
	JudgeProvider          string             `json:"judgeProvider,omitempty"`
	JudgeModel             string             `json:"judgeModel,omitempty"`
	JudgeAPIKeyEnv         string             `json:"judgeApiKeyEnv,omitempty"`
	JudgeBaseURL           string             `json:"judgeBaseUrl,omitempty"`
	JudgeBaseURLEnv        string             `json:"judgeBaseUrlEnv,omitempty"`
	UserModelProvider      string             `json:"userModelProvider,omitempty"`
	UserModel              string             `json:"userModel,omitempty"`
	UserModelAPIKeyEnv     string             `json:"userModelApiKeyEnv,omitempty"`
	UserModelBaseURL       string             `json:"userModelBaseUrl,omitempty"`
	UserModelBaseURLEnv    string             `json:"userModelBaseUrlEnv,omitempty"`
	ExtraEnv               map[string]string  `json:"extraEnv,omitempty"`
}

type TemplateData struct {
	Catalog      Catalog
	Benchmark    BenchmarkSpec
	Lane         LaneSpec
	Mode         string
	Workspace    string
	BenchmarkDir string
	OutputDir    string
	Timestamp    string
	TrialCount   int
}

func LoadCatalog(path string) (Catalog, error) {
	if path == "" {
		path = DefaultCatalogPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	if catalog.DefaultOutputDir == "" {
		catalog.DefaultOutputDir = filepath.Join("benchmarks", "results")
	}
	return catalog, ValidateCatalog(catalog)
}

func ValidateCatalog(catalog Catalog) error {
	if strings.TrimSpace(catalog.Version) == "" {
		return errors.New("catalog version is required")
	}
	if len(catalog.Benchmarks) == 0 {
		return errors.New("catalog must contain at least one benchmark")
	}
	if len(catalog.Lanes) == 0 {
		return errors.New("catalog must contain at least one lane")
	}
	benchmarkIDs := map[string]struct{}{}
	for _, bench := range catalog.Benchmarks {
		if strings.TrimSpace(bench.ID) == "" {
			return errors.New("benchmark id is required")
		}
		if _, exists := benchmarkIDs[bench.ID]; exists {
			return fmt.Errorf("duplicate benchmark id: %s", bench.ID)
		}
		benchmarkIDs[bench.ID] = struct{}{}
		if strings.TrimSpace(bench.Name) == "" {
			return fmt.Errorf("benchmark %s is missing name", bench.ID)
		}
		if len(bench.PrimaryMetrics) == 0 {
			return fmt.Errorf("benchmark %s must declare at least one primary metric", bench.ID)
		}
		if len(bench.SmokeCommands) == 0 && len(bench.NightlyCommands) == 0 {
			return fmt.Errorf("benchmark %s must declare smokeCommands or nightlyCommands", bench.ID)
		}
		if bench.OfficialRepoURL == "" && bench.OfficialPaperURL == "" {
			return fmt.Errorf("benchmark %s must declare an official repo or paper url", bench.ID)
		}
		if bench.OfficialRepoURL != "" && bench.OfficialRef == "" {
			return fmt.Errorf("benchmark %s must pin officialRef when officialRepoUrl is set", bench.ID)
		}
		defaultCount := 0
		baselineLabels := map[string]struct{}{}
		for _, baseline := range bench.Baselines {
			if strings.TrimSpace(baseline.Label) == "" {
				return fmt.Errorf("benchmark %s has baseline with empty label", bench.ID)
			}
			if _, exists := baselineLabels[baseline.Label]; exists {
				return fmt.Errorf("benchmark %s has duplicate baseline label %s", bench.ID, baseline.Label)
			}
			baselineLabels[baseline.Label] = struct{}{}
			if baseline.Default {
				defaultCount++
			}
		}
		if defaultCount > 1 {
			return fmt.Errorf("benchmark %s has multiple default baselines", bench.ID)
		}
	}
	laneIDs := map[string]struct{}{}
	for _, lane := range catalog.Lanes {
		if strings.TrimSpace(lane.ID) == "" {
			return errors.New("lane id is required")
		}
		if _, exists := laneIDs[lane.ID]; exists {
			return fmt.Errorf("duplicate lane id: %s", lane.ID)
		}
		laneIDs[lane.ID] = struct{}{}
		if strings.TrimSpace(lane.Provider) == "" {
			return fmt.Errorf("lane %s is missing provider", lane.ID)
		}
		if strings.TrimSpace(lane.Model) == "" {
			return fmt.Errorf("lane %s is missing model", lane.ID)
		}
		if lane.PromptCostPer1KUSD < 0 || lane.CompletionCostPer1KUSD < 0 || lane.MaxTokens < 0 || lane.MaxToolCalls < 0 || lane.MaxCostUSD < 0 || lane.LatencyToleranceMs < 0 || lane.CostToleranceUSD < 0 {
			return fmt.Errorf("lane %s has negative live-lane controls", lane.ID)
		}
		for metric, tolerance := range lane.MetricTolerances {
			if strings.TrimSpace(metric) == "" {
				return fmt.Errorf("lane %s has empty metric tolerance key", lane.ID)
			}
			if tolerance < 0 {
				return fmt.Errorf("lane %s has negative tolerance for %s", lane.ID, metric)
			}
		}
		for _, secret := range lane.RequiredSecrets {
			if strings.TrimSpace(secret) == "" {
				return fmt.Errorf("lane %s has empty required secret", lane.ID)
			}
		}
	}
	return nil
}

func (catalog Catalog) Benchmark(id string) (BenchmarkSpec, bool) {
	for _, bench := range catalog.Benchmarks {
		if bench.ID == id {
			return bench, true
		}
	}
	return BenchmarkSpec{}, false
}

func (catalog Catalog) Lane(id string) (LaneSpec, bool) {
	for _, lane := range catalog.Lanes {
		if lane.ID == id {
			return lane, true
		}
	}
	return LaneSpec{}, false
}

func (catalog Catalog) Summary() map[string]any {
	benchmarks := make([]map[string]any, 0, len(catalog.Benchmarks))
	for _, bench := range catalog.Benchmarks {
		benchmarks = append(benchmarks, map[string]any{
			"id":             bench.ID,
			"name":           bench.Name,
			"tier":           bench.Tier,
			"family":         bench.Family,
			"primaryMetrics": bench.PrimaryMetrics,
		})
	}
	lanes := make([]map[string]any, 0, len(catalog.Lanes))
	for _, lane := range catalog.Lanes {
		lanes = append(lanes, map[string]any{
			"id":       lane.ID,
			"provider": lane.Provider,
			"model":    lane.Model,
		})
	}
	sort.Slice(benchmarks, func(i, j int) bool { return benchmarks[i]["id"].(string) < benchmarks[j]["id"].(string) })
	sort.Slice(lanes, func(i, j int) bool { return lanes[i]["id"].(string) < lanes[j]["id"].(string) })
	return map[string]any{
		"version":          catalog.Version,
		"defaultOutputDir": catalog.DefaultOutputDir,
		"benchmarkCount":   len(catalog.Benchmarks),
		"laneCount":        len(catalog.Lanes),
		"benchmarks":       benchmarks,
		"lanes":            lanes,
	}
}

func (benchmark BenchmarkSpec) CommandsForMode(mode string) ([]string, error) {
	switch mode {
	case "smoke":
		return benchmark.SmokeCommands, nil
	case "nightly":
		return benchmark.NightlyCommands, nil
	default:
		return nil, fmt.Errorf("unsupported mode: %s", mode)
	}
}

func (benchmark BenchmarkSpec) Baseline(label string) (BaselineSnapshot, bool) {
	if label != "" {
		for _, baseline := range benchmark.Baselines {
			if baseline.Label == label {
				return baseline, true
			}
		}
		return BaselineSnapshot{}, false
	}
	for _, baseline := range benchmark.Baselines {
		if baseline.Default {
			return baseline, true
		}
	}
	if len(benchmark.Baselines) == 1 {
		return benchmark.Baselines[0], true
	}
	return BaselineSnapshot{}, false
}

func ResolveCommands(commands []string, data TemplateData) ([]string, error) {
	resolved := make([]string, 0, len(commands))
	for _, command := range commands {
		tpl, err := template.New("command").Option("missingkey=error").Parse(command)
		if err != nil {
			return nil, fmt.Errorf("parse command template %q: %w", command, err)
		}
		var builder strings.Builder
		if err := tpl.Execute(&builder, data); err != nil {
			return nil, fmt.Errorf("render command template %q: %w", command, err)
		}
		resolved = append(resolved, builder.String())
	}
	return resolved, nil
}
