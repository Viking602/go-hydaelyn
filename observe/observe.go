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
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/provider"
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
	attrs = redactAttrs(attrs)
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

type OperationKind string

const (
	OperationStage           OperationKind = "stage"
	OperationCapability      OperationKind = "capability"
	OperationOutputGuardrail OperationKind = "output_guardrail"
)

type OperationInput struct {
	Kind      OperationKind
	Name      string
	Counter   string
	Stage     string
	Operation string
	Metadata  map[string]string
}

func (o OperationInput) Attrs() map[string]string {
	attrs := cloneAttrs(o.Metadata)
	if attrs == nil {
		attrs = map[string]string{}
	}
	if o.Kind != "" {
		attrs["kind"] = string(o.Kind)
	}
	if o.Stage != "" {
		attrs["stage"] = o.Stage
	}
	if o.Operation != "" {
		attrs["operation"] = o.Operation
	}
	return attrs
}

func ObserveOperation(ctx context.Context, observer Observer, input OperationInput, fn func(context.Context, map[string]string) error) error {
	attrs := input.Attrs()
	if traceID := TraceID(ctx); traceID != "" {
		attrs["trace_id"] = traceID
	}
	spanCtx, span := observer.StartSpan(input.Name, attrs)
	defer span.End()
	counter := input.Counter
	if counter == "" {
		counter = input.Name
	}
	observer.IncCounter(counter+".calls", 1, attrs)
	err := fn(spanCtx, attrs)
	if err != nil {
		logAttrs := cloneAttrs(attrs)
		logAttrs["trace_id"] = TraceID(spanCtx)
		observer.Log("error", err.Error(), logAttrs)
	}
	return err
}

func RuntimeMiddleware(observer Observer) middleware.Handler {
	return middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		metadata := map[string]string{}
		if envelope.TeamID != "" {
			metadata["team_id"] = envelope.TeamID
		}
		if envelope.TaskID != "" {
			metadata["task_id"] = envelope.TaskID
		}
		if envelope.AgentID != "" {
			metadata["agent_id"] = envelope.AgentID
		}
		for key, value := range envelope.Metadata {
			if value != "" {
				metadata[key] = value
			}
		}
		return ObserveOperation(ctx, observer, OperationInput{
			Kind:      OperationStage,
			Name:      string(envelope.Stage) + "." + envelope.Operation,
			Counter:   string(envelope.Stage),
			Stage:     string(envelope.Stage),
			Operation: envelope.Operation,
			Metadata:  metadata,
		}, func(spanCtx context.Context, attrs map[string]string) error {
			if counter := envelope.Metadata["collaboration_counter"]; counter != "" {
				observer.IncCounter(counter, 1, attrs)
			}
			if event := envelope.Metadata["collaboration_event"]; event != "" {
				logAttrs := cloneAttrs(attrs)
				logAttrs["trace_id"] = TraceID(spanCtx)
				observer.Log("info", event, logAttrs)
			}
			if err := next(spanCtx, envelope); err != nil {
				return err
			}
			return nil
		})
	})
}

func CapabilityMiddleware(observer Observer) capability.Middleware {
	return capabilityObserverMiddleware{observer: observer}
}

type capabilityObserverMiddleware struct {
	observer Observer
}

func (m capabilityObserverMiddleware) Handle(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
	var result capability.Result
	err := ObserveOperation(ctx, m.observer, OperationInput{
		Kind:      OperationCapability,
		Name:      string(call.Type) + "." + call.Name,
		Counter:   string(call.Type),
		Stage:     string(call.Type),
		Operation: "invoke",
		Metadata: map[string]string{
			"type": string(call.Type),
			"name": call.Name,
		},
	}, func(spanCtx context.Context, attrs map[string]string) error {
		current, err := next(spanCtx, call)
		if err != nil {
			return err
		}
		result = current
		m.observer.ObserveHistogram(string(call.Type)+".duration_ms", float64(result.Usage.Duration.Milliseconds()), attrs)
		return nil
	})
	return result, err
}

func (m capabilityObserverMiddleware) HandleStream(ctx context.Context, call capability.Call, next capability.StreamNext) (provider.Stream, error) {
	attrs := map[string]string{
		"type": string(call.Type),
		"name": call.Name,
	}
	spanCtx, span := m.observer.StartSpan(string(call.Type)+"."+call.Name, attrs)
	m.observer.IncCounter(string(call.Type)+".calls", 1, attrs)
	started := time.Now()
	stream, err := next(spanCtx, call)
	if err != nil {
		logAttrs := cloneAttrs(attrs)
		logAttrs["trace_id"] = TraceID(spanCtx)
		m.observer.Log("error", err.Error(), logAttrs)
		span.End()
		return nil, err
	}
	return &capabilityStreamObserver{
		Stream:    stream,
		observer:  m.observer,
		attrs:     attrs,
		spanCtx:   spanCtx,
		span:      span,
		startedAt: started,
	}, nil
}

type capabilityStreamObserver struct {
	provider.Stream
	observer  Observer
	attrs     map[string]string
	spanCtx   context.Context
	span      Span
	startedAt time.Time
	doneOnce  sync.Once
}

func (s *capabilityStreamObserver) Recv() (provider.Event, error) {
	event, err := s.Stream.Recv()
	if err != nil {
		s.finish()
		return event, err
	}
	if event.Kind == provider.EventDone {
		s.finish()
	}
	return event, err
}

func (s *capabilityStreamObserver) Close() error {
	err := s.Stream.Close()
	s.finish()
	return err
}

func (s *capabilityStreamObserver) finish() {
	s.doneOnce.Do(func() {
		s.observer.ObserveHistogram(s.attrs["type"]+".duration_ms", float64(time.Since(s.startedAt).Milliseconds()), s.attrs)
		s.span.End()
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

func RedactSecrets(value string) (string, bool) {
	redacted := redact(value)
	return redacted, redacted != value
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
