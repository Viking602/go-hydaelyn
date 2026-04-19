package observe

import (
	"context"
	"testing"
)

func TestSecretLeak(t *testing.T) {
	t.Parallel()

	secret := "sk-secret1234567890"
	observer := NewMemoryObserver()
	_, span := observer.StartSpan("tool.exec", map[string]string{"argument": secret, "note": "prefix-" + secret})
	span.End()
	observer.Log("error", "leaked "+secret, map[string]string{"token": secret})

	spans := observer.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %#v", spans)
	}
	if spans[0].Attrs["argument"] == secret || spans[0].Attrs["note"] == "prefix-"+secret {
		t.Fatalf("expected span attrs redacted, got %#v", spans[0].Attrs)
	}
	logs := observer.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %#v", logs)
	}
	if logs[0].Message == "leaked "+secret || logs[0].Attrs["token"] == secret {
		t.Fatalf("expected log redaction, got %#v", logs[0])
	}

	var spanAttrs map[string]string
	var counterAttrs map[string]string
	var histogramAttrs map[string]string
	adapter := NewOTelAdapter(OTelConfig{
		StartSpanFunc: func(_ string, attrs map[string]string) (context.Context, Span) {
			spanAttrs = attrs
			return context.Background(), noopSpan{}
		},
		IncCounterFunc: func(_ string, _ int64, attrs map[string]string) {
			counterAttrs = attrs
		},
		ObserveHistogramFunc: func(_ string, _ float64, attrs map[string]string) {
			histogramAttrs = attrs
		},
	})
	adapter.StartSpan("tool.exec", map[string]string{"argument": secret})
	adapter.IncCounter("tool.calls", 1, map[string]string{"argument": secret})
	adapter.ObserveHistogram("tool.duration_ms", 1, map[string]string{"argument": secret})
	for name, attrs := range map[string]map[string]string{"span": spanAttrs, "counter": counterAttrs, "histogram": histogramAttrs} {
		if attrs["argument"] == secret {
			t.Fatalf("expected %s attrs redacted, got %#v", name, attrs)
		}
	}
}
