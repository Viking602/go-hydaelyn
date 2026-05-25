package jsonrpc

import (
	"bufio"
	"bytes"
	"strings"
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

func TestReadFramedRejectsTooLargeContentLength(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Length: 10485760\r\n\r\n"))
	_, err := ReadFramed(reader)
	if err == nil {
		t.Fatal("expected too-large frame error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large frame error, got %v", err)
	}
}
