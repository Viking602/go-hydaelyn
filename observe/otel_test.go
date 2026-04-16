package observe

import (
	"context"
	"testing"
)

func TestOTelAdapterDelegatesToCallbacks(t *testing.T) {
	var spans []string
	var counters []string
	var histograms []float64
	var logs []string

	adapter := NewOTelAdapter(OTelConfig{
		StartSpanFunc: func(name string, _ map[string]string) (context.Context, Span) {
			spans = append(spans, name)
			return context.Background(), noopSpan{}
		},
		IncCounterFunc: func(name string, _ int64, _ map[string]string) {
			counters = append(counters, name)
		},
		ObserveHistogramFunc: func(_ string, value float64, _ map[string]string) {
			histograms = append(histograms, value)
		},
		LogFunc: func(_, message string, _ map[string]string) {
			logs = append(logs, message)
		},
	})

	adapter.StartSpan("team.start", nil)
	adapter.IncCounter("task.success", 1, nil)
	adapter.ObserveHistogram("tool.latency_ms", 42.0, nil)
	adapter.Log("info", "hello", nil)

	if len(spans) != 1 || spans[0] != "team.start" {
		t.Fatalf("unexpected spans %v", spans)
	}
	if len(counters) != 1 || counters[0] != "task.success" {
		t.Fatalf("unexpected counters %v", counters)
	}
	if len(histograms) != 1 || histograms[0] != 42.0 {
		t.Fatalf("unexpected histograms %v", histograms)
	}
	if len(logs) != 1 || logs[0] != "hello" {
		t.Fatalf("unexpected logs %v", logs)
	}
}

func TestOTelAdapterRedactsSecrets(t *testing.T) {
	var logged string
	adapter := NewOTelAdapter(OTelConfig{
		LogFunc: func(_, message string, _ map[string]string) {
			logged = message
		},
	})
	adapter.Log("error", "leaked sk-secret1234567890", nil)
	if logged == "leaked sk-secret1234567890" {
		t.Fatalf("expected redacted log, got %q", logged)
	}
}

func TestOTelAdapterNilCallbacksDoNotPanic(t *testing.T) {
	adapter := NewOTelAdapter(OTelConfig{})
	ctx, span := adapter.StartSpan("test", nil)
	span.End()
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	adapter.IncCounter("x", 1, nil)
	adapter.ObserveHistogram("x", 1, nil)
	adapter.Log("info", "msg", nil)
}
