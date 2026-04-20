// Package slow provides instrumentation for operations that may become
// unexpectedly expensive (JSON marshal/unmarshal, session loads, blackboard
// aggregation, etc.). It mirrors the slow-operation logging patterns found in
// production agent CLI systems.
package slow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/Viking602/go-hydaelyn/observe"
)

var (
	// Threshold is the duration after which an operation is considered "slow"
	// and should be logged. It can be overridden with the environment variable
	// HYDAELYN_SLOW_OPERATION_THRESHOLD_MS.
	Threshold = defaultThreshold()

	// NoopObserver is used when no external observer is provided.
	NoopObserver observe.Observer = &noopObserver{}
)

func defaultThreshold() time.Duration {
	if v := os.Getenv("HYDAELYN_SLOW_OPERATION_THRESHOLD_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 300 * time.Millisecond
}

// Operation wraps a named function and logs a warning if it exceeds Threshold.
// The optional observer receives a structured log record.
func Operation(name string, observer observe.Observer, fn func()) {
	if observer == nil {
		observer = NoopObserver
	}
	start := time.Now()
	fn()
	d := time.Since(start)
	if d >= Threshold {
		_, file, line, _ := runtime.Caller(2)
		observer.Log("warn", fmt.Sprintf("slow operation: %s took %v", name, d), map[string]string{
			"operation": name,
			"duration":  d.String(),
			"caller":    fmt.Sprintf("%s:%d", file, line),
		})
	}
}

// MarshalJSON is a convenience wrapper around json.Marshal that instruments
// the call.
func MarshalJSON(v any, observer observe.Observer) ([]byte, error) {
	var result []byte
	var err error
	Operation("json.Marshal", observer, func() {
		result, err = json.Marshal(v)
	})
	return result, err
}

// MarshalIndentJSON is a convenience wrapper around json.MarshalIndent that
// instruments the call.
func MarshalIndentJSON(v any, prefix, indent string, observer observe.Observer) ([]byte, error) {
	var result []byte
	var err error
	Operation("json.MarshalIndent", observer, func() {
		result, err = json.MarshalIndent(v, prefix, indent)
	})
	return result, err
}

// UnmarshalJSON is a convenience wrapper around json.Unmarshal that instruments
// the call.
func UnmarshalJSON(data []byte, v any, observer observe.Observer) error {
	var err error
	Operation("json.Unmarshal", observer, func() {
		err = json.Unmarshal(data, v)
	})
	return err
}

type noopObserver struct{}

func (n *noopObserver) StartSpan(name string, attrs map[string]string) (context.Context, observe.Span) {
	return context.Background(), &noopSpan{}
}

func (n *noopObserver) IncCounter(name string, delta int64, attrs map[string]string) {}

func (n *noopObserver) ObserveHistogram(name string, value float64, attrs map[string]string) {}

func (n *noopObserver) Log(level, message string, attrs map[string]string) {}

type noopSpan struct{}

func (n *noopSpan) End() {}
