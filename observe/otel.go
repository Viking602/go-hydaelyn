package observe

import "context"

// OTelConfig bridges the Observer interface to any OpenTelemetry-compatible
// backend. Callers supply callback functions that delegate to their own
// OTel tracer, meter, and logger instances. This keeps the core module free
// of external dependencies while providing a first-class integration path.
//
// Example wiring with go.opentelemetry.io/otel:
//
//	tracer := otel.Tracer("hydaelyn")
//	meter  := otel.Meter("hydaelyn")
//
//	adapter := observe.NewOTelAdapter(observe.OTelConfig{
//	    StartSpanFunc: func(name string, attrs map[string]string) (context.Context, observe.Span) {
//	        ctx, span := tracer.Start(context.Background(), name)
//	        for k, v := range attrs { span.SetAttributes(attribute.String(k, v)) }
//	        return ctx, &otelSpan{span}
//	    },
//	    IncCounterFunc: func(name string, delta int64, attrs map[string]string) {
//	        counter, _ := meter.Int64Counter(name)
//	        counter.Add(context.Background(), delta)
//	    },
//	    ObserveHistogramFunc: func(name string, value float64, attrs map[string]string) {
//	        hist, _ := meter.Float64Histogram(name)
//	        hist.Record(context.Background(), value)
//	    },
//	    LogFunc: func(level, message string, attrs map[string]string) {
//	        slog.Log(context.Background(), toSlogLevel(level), message)
//	    },
//	})
type OTelConfig struct {
	StartSpanFunc        func(name string, attrs map[string]string) (context.Context, Span)
	IncCounterFunc       func(name string, delta int64, attrs map[string]string)
	ObserveHistogramFunc func(name string, value float64, attrs map[string]string)
	LogFunc              func(level, message string, attrs map[string]string)
}

type otelAdapter struct {
	cfg OTelConfig
}

// NewOTelAdapter creates an Observer that delegates to user-provided OTel
// callbacks. Any nil callback falls through to a no-op.
func NewOTelAdapter(cfg OTelConfig) Observer {
	return &otelAdapter{cfg: cfg}
}

func (a *otelAdapter) StartSpan(name string, attrs map[string]string) (context.Context, Span) {
	if a.cfg.StartSpanFunc != nil {
		return a.cfg.StartSpanFunc(name, attrs)
	}
	return context.Background(), noopSpan{}
}

func (a *otelAdapter) IncCounter(name string, delta int64, attrs map[string]string) {
	if a.cfg.IncCounterFunc != nil {
		a.cfg.IncCounterFunc(name, delta, attrs)
	}
}

func (a *otelAdapter) ObserveHistogram(name string, value float64, attrs map[string]string) {
	if a.cfg.ObserveHistogramFunc != nil {
		a.cfg.ObserveHistogramFunc(name, value, attrs)
	}
}

func (a *otelAdapter) Log(level, message string, attrs map[string]string) {
	if a.cfg.LogFunc != nil {
		a.cfg.LogFunc(level, redact(message), redactAttrs(attrs))
	}
}

type noopSpan struct{}

func (noopSpan) End() {}
