package benchmark

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type MetricComparison struct {
	Metric   string   `json:"metric"`
	Actual   *float64 `json:"actual,omitempty"`
	Baseline *float64 `json:"baseline,omitempty"`
	Delta    *float64 `json:"delta,omitempty"`
}

type ComparisonReport struct {
	BenchmarkID       string             `json:"benchmarkId"`
	BenchmarkName     string             `json:"benchmarkName"`
	LaneID            string             `json:"laneId"`
	Provider          string             `json:"provider"`
	Model             string             `json:"model"`
	Mode              string             `json:"mode"`
	Timestamp         time.Time          `json:"timestamp"`
	HarnessRef        string             `json:"harnessRef,omitempty"`
	BaselineLabel     string             `json:"baselineLabel,omitempty"`
	BaselineSourceURL string             `json:"baselineSourceUrl,omitempty"`
	Comparisons       []MetricComparison `json:"comparisons"`
}

func BuildComparisonReport(bench BenchmarkSpec, lane LaneSpec, mode string, timestamp time.Time, actualScores map[string]float64, baseline *BaselineSnapshot) ComparisonReport {
	orderedMetrics := metricOrder(bench.PrimaryMetrics, actualScores, baseline)
	report := ComparisonReport{
		BenchmarkID:   bench.ID,
		BenchmarkName: bench.Name,
		LaneID:        lane.ID,
		Provider:      lane.Provider,
		Model:         lane.Model,
		Mode:          mode,
		Timestamp:     timestamp.UTC(),
		HarnessRef:    bench.OfficialRef,
	}
	if baseline != nil {
		report.BaselineLabel = baseline.Label
		report.BaselineSourceURL = baseline.SourceURL
	}
	for _, metric := range orderedMetrics {
		comparison := MetricComparison{Metric: metric}
		if score, ok := actualScores[metric]; ok {
			value := score
			comparison.Actual = &value
		}
		if baseline != nil {
			if score, ok := baseline.Scores[metric]; ok {
				value := score
				comparison.Baseline = &value
				if comparison.Actual != nil {
					delta := *comparison.Actual - value
					comparison.Delta = &delta
				}
			}
		}
		report.Comparisons = append(report.Comparisons, comparison)
	}
	return report
}

func RenderComparisonMarkdown(report ComparisonReport) string {
	lines := []string{
		fmt.Sprintf("# %s", report.BenchmarkName),
		"",
		fmt.Sprintf("- Lane: `%s`", report.LaneID),
		fmt.Sprintf("- Provider / model: `%s` / `%s`", report.Provider, report.Model),
		fmt.Sprintf("- Mode: `%s`", report.Mode),
		fmt.Sprintf("- Timestamp: `%s`", report.Timestamp.Format(time.RFC3339)),
	}
	if report.HarnessRef != "" {
		lines = append(lines, fmt.Sprintf("- Harness ref: `%s`", report.HarnessRef))
	}
	if report.BaselineLabel != "" {
		lines = append(lines, fmt.Sprintf("- Baseline: `%s`", report.BaselineLabel))
	}
	if report.BaselineSourceURL != "" {
		lines = append(lines, fmt.Sprintf("- Baseline source: %s", report.BaselineSourceURL))
	}
	lines = append(lines, "", "| Metric | Hydaelyn | Baseline | Delta |", "|---|---:|---:|---:|")
	for _, comparison := range report.Comparisons {
		lines = append(lines, fmt.Sprintf(
			"| %s | %s | %s | %s |",
			comparison.Metric,
			formatMetricValue(comparison.Actual),
			formatMetricValue(comparison.Baseline),
			formatMetricValue(comparison.Delta),
		))
	}
	return strings.Join(lines, "\n")
}

func formatMetricValue(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *value)
}

func metricOrder(primary []string, actual map[string]float64, baseline *BaselineSnapshot) []string {
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(primary)+len(actual))
	for _, metric := range primary {
		if _, ok := seen[metric]; ok {
			continue
		}
		seen[metric] = struct{}{}
		ordered = append(ordered, metric)
	}
	extras := make([]string, 0, len(actual))
	for metric := range actual {
		if _, ok := seen[metric]; ok {
			continue
		}
		seen[metric] = struct{}{}
		extras = append(extras, metric)
	}
	if baseline != nil {
		for metric := range baseline.Scores {
			if _, ok := seen[metric]; ok {
				continue
			}
			seen[metric] = struct{}{}
			extras = append(extras, metric)
		}
	}
	sort.Strings(extras)
	ordered = append(ordered, extras...)
	return ordered
}

func MarshalIndented(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
