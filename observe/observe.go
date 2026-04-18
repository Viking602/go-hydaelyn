package observe

import (
	"context"
	"maps"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/middleware"
)

type Span interface {
	End()
}

type Observer interface {
	StartSpan(name string, attrs map[string]string) (context.Context, Span)
	IncCounter(name string, delta int64, attrs map[string]string)
	ObserveHistogram(name string, value float64, attrs map[string]string)
	Log(level, message string, attrs map[string]string)
}

type RecordedSpan struct {
	Name      string            `json:"name"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	StartedAt time.Time         `json:"startedAt"`
	EndedAt   time.Time         `json:"endedAt"`
}

type memoryObserver struct {
	mu         sync.RWMutex
	spans      []RecordedSpan
	counters   map[string]int64
	histograms map[string][]float64
	logs       []LogRecord
	seq        uint64
}

var secretPattern = regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`)

type LogRecord struct {
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

func NewMemoryObserver() *memoryObserver {
	return &memoryObserver{
		counters:   map[string]int64{},
		histograms: map[string][]float64{},
	}
}

func (m *memoryObserver) StartSpan(name string, attrs map[string]string) (context.Context, Span) {
	attrs = cloneAttrs(attrs)
	traceID := ""
	if attrs != nil {
		traceID = attrs["trace_id"]
	}
	if traceID == "" {
		traceID = nextTraceID(&m.seq)
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["trace_id"] = traceID
	span := &memorySpan{
		parent:  m,
		name:    name,
		attrs:   attrs,
		started: time.Now().UTC(),
	}
	return context.WithValue(context.Background(), traceIDKey{}, traceID), span
}

func (m *memoryObserver) IncCounter(name string, delta int64, _ map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[name] += delta
}

func (m *memoryObserver) ObserveHistogram(name string, value float64, _ map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms[name] = append(m.histograms[name], value)
}

func (m *memoryObserver) Spans() []RecordedSpan {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]RecordedSpan{}, m.spans...)
}

func (m *memoryObserver) Counters() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make(map[string]int64, len(m.counters))
	maps.Copy(items, m.counters)
	return items
}

func (m *memoryObserver) Histograms() map[string][]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make(map[string][]float64, len(m.histograms))
	for key, values := range m.histograms {
		items[key] = append([]float64{}, values...)
	}
	return items
}

func (m *memoryObserver) Log(level, message string, attrs map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, LogRecord{
		Level:     level,
		Message:   redact(message),
		Attrs:     redactAttrs(attrs),
		CreatedAt: time.Now().UTC(),
	})
}

func (m *memoryObserver) Logs() []LogRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]LogRecord{}, m.logs...)
}

type memorySpan struct {
	parent  *memoryObserver
	name    string
	attrs   map[string]string
	started time.Time
}

func (s *memorySpan) End() {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	s.parent.spans = append(s.parent.spans, RecordedSpan{
		Name:      s.name,
		Attrs:     cloneAttrs(s.attrs),
		StartedAt: s.started,
		EndedAt:   time.Now().UTC(),
	})
}

func RuntimeMiddleware(observer Observer) middleware.Handler {
	return middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		attrs := map[string]string{
			"stage": string(envelope.Stage),
		}
		if envelope.TeamID != "" {
			attrs["team_id"] = envelope.TeamID
		}
		if envelope.TaskID != "" {
			attrs["task_id"] = envelope.TaskID
		}
		if envelope.AgentID != "" {
			attrs["agent_id"] = envelope.AgentID
		}
		for key, value := range envelope.Metadata {
			if value != "" {
				attrs[key] = value
			}
		}
		if traceID := TraceID(ctx); traceID != "" {
			attrs["trace_id"] = traceID
		}
		spanCtx, span := observer.StartSpan(string(envelope.Stage)+"."+envelope.Operation, attrs)
		defer span.End()
		observer.IncCounter(string(envelope.Stage)+".calls", 1, attrs)
		if counter := envelope.Metadata["collaboration_counter"]; counter != "" {
			observer.IncCounter(counter, 1, attrs)
		}
		if event := envelope.Metadata["collaboration_event"]; event != "" {
			logAttrs := cloneAttrs(attrs)
			logAttrs["trace_id"] = TraceID(spanCtx)
			observer.Log("info", event, logAttrs)
		}
		err := next(spanCtx, envelope)
		if err != nil {
			logAttrs := cloneAttrs(attrs)
			logAttrs["trace_id"] = TraceID(spanCtx)
			observer.Log("error", err.Error(), logAttrs)
		}
		return err
	})
}

func CapabilityMiddleware(observer Observer) capability.Middleware {
	return capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		attrs := map[string]string{
			"type": string(call.Type),
			"name": call.Name,
		}
		spanCtx, span := observer.StartSpan(string(call.Type)+"."+call.Name, attrs)
		defer span.End()
		observer.IncCounter(string(call.Type)+".calls", 1, attrs)
		result, err := next(spanCtx, call)
		if err == nil {
			observer.ObserveHistogram(string(call.Type)+".duration_ms", float64(result.Usage.Duration.Milliseconds()), attrs)
		} else {
			logAttrs := cloneAttrs(attrs)
			logAttrs["trace_id"] = TraceID(spanCtx)
			observer.Log("error", err.Error(), logAttrs)
		}
		return result, err
	})
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	items := make(map[string]string, len(attrs))
	maps.Copy(items, attrs)
	return items
}

func redact(value string) string {
	return secretPattern.ReplaceAllString(value, "[REDACTED]")
}

func redactAttrs(attrs map[string]string) map[string]string {
	items := cloneAttrs(attrs)
	for key, value := range items {
		items[key] = redact(value)
	}
	return items
}

type traceIDKey struct{}

func TraceID(ctx context.Context) string {
	if value, ok := ctx.Value(traceIDKey{}).(string); ok {
		return value
	}
	return ""
}

func nextTraceID(seq *uint64) string {
	return "trace-" + time.Now().UTC().Format("150405") + "-" + formatUint(atomic.AddUint64(seq, 1))
}

func formatUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
