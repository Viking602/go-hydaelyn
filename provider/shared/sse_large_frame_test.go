package shared

import (
	"io"
	"strings"
	"testing"
)

func TestReaderRejectsLineOverMax(t *testing.T) {
	payload := strings.Repeat("a", MaxSSELineBytes+1)
	reader := NewReader(strings.NewReader("data: " + payload + "\n\n"))
	if _, err := reader.Next(); err == nil {
		t.Fatal("expected oversized line error")
	}
}

func TestReaderRejectsAggregateFrameOverLineLimit(t *testing.T) {
	frame := strings.Repeat("data: a\n", MaxSSEFrameLines+1) + "\n"
	reader := NewReader(strings.NewReader(frame))
	if _, err := reader.Next(); err == nil || !strings.Contains(err.Error(), "frame exceeds") {
		t.Fatalf("aggregate frame error = %v", err)
	}
}

func TestReaderHandlesDataLineLargerThanOneMiB(t *testing.T) {
	payload := strings.Repeat("a", 2*1024*1024)
	reader := NewReader(strings.NewReader("data: " + payload + "\n\n"))

	evt, err := reader.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(evt.Data) != len(payload) {
		t.Fatalf("expected payload length %d, got %d", len(payload), len(evt.Data))
	}
	if evt.Data[:32] != payload[:32] || evt.Data[len(evt.Data)-32:] != payload[len(payload)-32:] {
		t.Fatalf("expected payload contents to survive large frame parsing")
	}

	_, err = reader.Next()
	if err != io.EOF {
		t.Fatalf("expected EOF after large frame, got %v", err)
	}
}
