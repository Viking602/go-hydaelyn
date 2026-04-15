package jsonrpc

import (
	"bufio"
	"bytes"
	"testing"
)

func TestFramedRoundTrip(t *testing.T) {
	buffer := &bytes.Buffer{}
	request, err := NewRequest(1, "ping", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if err := WriteFramed(buffer, request); err != nil {
		t.Fatalf("WriteFramed() error = %v", err)
	}
	payload, err := ReadFramed(bufio.NewReader(buffer))
	if err != nil {
		t.Fatalf("ReadFramed() error = %v", err)
	}
	decoded, err := DecodeRequest(payload)
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if decoded.Method != "ping" {
		t.Fatalf("expected ping, got %q", decoded.Method)
	}
}
