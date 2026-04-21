package shared

import (
	"io"
	"strings"
	"testing"
)

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
