package errors

import (
	"errors"
	"testing"
)

func TestAbortError(t *testing.T) {
	err := &AbortError{Reason: "user cancelled"}
	if got := err.Error(); got != "aborted: user cancelled" {
		t.Errorf("unexpected error message: %q", got)
	}
	if !IsAbort(err) {
		t.Error("expected IsAbort to be true")
	}
	if IsAbort(errors.New("other")) {
		t.Error("expected IsAbort to be false for unrelated error")
	}
}

func TestShellError(t *testing.T) {
	err := &ShellError{Stdout: "out", Stderr: "err", Code: 1, Interrupted: false}
	if got := err.Error(); got != "shell command failed with code 1" {
		t.Errorf("unexpected error message: %q", got)
	}
}

func TestTelemetrySafeError(t *testing.T) {
	err := NewTelemetrySafeWithDetail("connection to /home/user failed", "connection failed")
	if got := err.Error(); got != "connection to /home/user failed" {
		t.Errorf("unexpected error message: %q", got)
	}
	if got := err.Telemetry(); got != "connection failed" {
		t.Errorf("unexpected telemetry message: %q", got)
	}
	if !IsTelemetrySafe(err) {
		t.Error("expected IsTelemetrySafe to be true")
	}
}

func TestToError(t *testing.T) {
	if err := ToError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	base := errors.New("base")
	if err := ToError(base); err != base {
		t.Error("expected same error instance")
	}
	if err := ToError("panic string"); err.Error() != "panic string" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestErrorMessage(t *testing.T) {
	if got := ErrorMessage(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
	if got := ErrorMessage(errors.New("oops")); got != "oops" {
		t.Errorf("unexpected message: %q", got)
	}
}
