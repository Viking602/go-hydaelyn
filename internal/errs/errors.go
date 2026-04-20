// Package errors provides structured error types used across the framework.
// These patterns are inspired by real-world agent CLI systems to enable
// precise error classification, telemetry safety, and user-facing messaging.
package errs

import (
	"errors"
	"fmt"
)

// AbortError indicates that an operation was cancelled by the user or by a
// parent context. It is not treated as a failure in telemetry.
type AbortError struct {
	Reason string
}

func (e *AbortError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("aborted: %s", e.Reason)
	}
	return "aborted"
}

// IsAbort reports whether err is an AbortError.
func IsAbort(err error) bool {
	var target *AbortError
	return errors.As(err, &target)
}

// ShellError indicates that a subprocess (e.g. Bash tool) exited with a
// non-zero status. It carries stdout, stderr, and the exit code so that
// the result can be passed back to the LLM as a tool result.
type ShellError struct {
	Stdout      string
	Stderr      string
	Code        int
	Interrupted bool
}

func (e *ShellError) Error() string {
	return fmt.Sprintf("shell command failed with code %d", e.Code)
}

// TelemetrySafeError wraps an error while guaranteeing that its message
// contains no sensitive data (file paths, URLs, code snippets). The
// TelemetryMessage field is safe to forward to observability pipelines.
//
// Usage:
//
//	return errors.NewTelemetrySafe("MCP server connection timed out")
//	return errors.NewTelemetrySafeWithDetail(fullMsg, telemetryMsg)
type TelemetrySafeError struct {
	Message          string
	TelemetryMessage string
}

func (e *TelemetrySafeError) Error() string {
	return e.Message
}

// Telemetry returns the message that is safe for logging to telemetry.
func (e *TelemetrySafeError) Telemetry() string {
	if e.TelemetryMessage != "" {
		return e.TelemetryMessage
	}
	return e.Message
}

// NewTelemetrySafe creates a telemetry-safe error with a single message.
func NewTelemetrySafe(message string) *TelemetrySafeError {
	return &TelemetrySafeError{Message: message, TelemetryMessage: message}
}

// NewTelemetrySafeWithDetail creates a telemetry-safe error where the full
// message may contain details (shown to the user) and telemetryMessage is
// the scrubbed version for logs.
func NewTelemetrySafeWithDetail(message, telemetryMessage string) *TelemetrySafeError {
	return &TelemetrySafeError{Message: message, TelemetryMessage: telemetryMessage}
}

// IsTelemetrySafe reports whether err is a TelemetrySafeError.
func IsTelemetrySafe(err error) bool {
	var target *TelemetrySafeError
	return errors.As(err, &target)
}

// ToError normalises an unknown value into a standard error.
func ToError(v any) error {
	if v == nil {
		return nil
	}
	if err, ok := v.(error); ok {
		return err
	}
	return fmt.Errorf("%v", v)
}

// ErrorMessage extracts a string message from an unknown error-like value.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
