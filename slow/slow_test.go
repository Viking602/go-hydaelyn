package slow

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/observe"
)

type spyObserver struct {
	logs []observe.LogRecord
}

func (s *spyObserver) StartSpan(name string, attrs map[string]string) (context.Context, observe.Span) {
	return context.Background(), &noopSpan{}
}

func (s *spyObserver) IncCounter(name string, delta int64, attrs map[string]string) {}

func (s *spyObserver) ObserveHistogram(name string, value float64, attrs map[string]string) {}

func (s *spyObserver) Log(level, message string, attrs map[string]string) {
	s.logs = append(s.logs, observe.LogRecord{Level: level, Message: message, Attrs: attrs})
}

func TestOperationDoesNotLogFastCalls(t *testing.T) {
	spy := &spyObserver{}
	oldThreshold := Threshold
	Threshold = 100 * time.Millisecond
	defer func() { Threshold = oldThreshold }()

	Operation("fast", spy, func() {
		time.Sleep(1 * time.Millisecond)
	})

	if len(spy.logs) != 0 {
		t.Errorf("expected no logs for fast operation, got %d", len(spy.logs))
	}
}

func TestOperationLogsSlowCalls(t *testing.T) {
	spy := &spyObserver{}
	oldThreshold := Threshold
	Threshold = 1 * time.Millisecond
	defer func() { Threshold = oldThreshold }()

	Operation("slow", spy, func() {
		time.Sleep(10 * time.Millisecond)
	})

	if len(spy.logs) != 1 {
		t.Fatalf("expected 1 log for slow operation, got %d", len(spy.logs))
	}
	if spy.logs[0].Level != "warn" {
		t.Errorf("expected warn level, got %s", spy.logs[0].Level)
	}
	if spy.logs[0].Attrs["operation"] != "slow" {
		t.Errorf("expected operation attr 'slow', got %s", spy.logs[0].Attrs["operation"])
	}
}

func TestMarshalJSON(t *testing.T) {
	spy := &spyObserver{}
	data, err := MarshalJSON(map[string]string{"key": "value"}, spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"key":"value"}` {
		t.Errorf("unexpected json: %s", string(data))
	}
}

func TestUnmarshalJSON(t *testing.T) {
	spy := &spyObserver{}
	var result map[string]string
	err := UnmarshalJSON([]byte(`{"key":"value"}`), &result, spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("unexpected value: %v", result)
	}
}
