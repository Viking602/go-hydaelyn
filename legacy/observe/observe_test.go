package observe

import "testing"

func TestMemoryObserverRecordsSpansAndCounters(t *testing.T) {
	observer := NewMemoryObserver()
	ctx, span := observer.StartSpan("team.start", map[string]string{"teamId": "team-1"})
	if ctx == nil {
		t.Fatalf("expected derived context")
	}
	span.End()
	observer.IncCounter("task.success", 1, map[string]string{"pattern": "deepsearch"})
	observer.ObserveHistogram("tool.latency_ms", 12.5, map[string]string{"tool": "search"})

	if len(observer.Spans()) != 1 || observer.Spans()[0].Name != "team.start" {
		t.Fatalf("unexpected spans %#v", observer.Spans())
	}
	if observer.Counters()["task.success"] != 1 {
		t.Fatalf("unexpected counters %#v", observer.Counters())
	}
	if len(observer.Histograms()["tool.latency_ms"]) != 1 {
		t.Fatalf("unexpected histograms %#v", observer.Histograms())
	}
	observer.Log("error", "permission denied", map[string]string{"trace_id": "trace-1"})
	if len(observer.Logs()) != 1 || observer.Logs()[0].Attrs["trace_id"] != "trace-1" {
		t.Fatalf("unexpected logs %#v", observer.Logs())
	}
	observer.Log("error", "leaked sk-secret1234567890", map[string]string{"token": "sk-secret1234567890"})
	logs := observer.Logs()
	if logs[1].Message == "leaked sk-secret1234567890" || logs[1].Attrs["token"] == "sk-secret1234567890" {
		t.Fatalf("expected redacted logs, got %#v", logs[1])
	}
}
