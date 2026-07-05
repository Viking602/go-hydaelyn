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

func TestReadFramed_MalformedHeaderReturnsParseError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Length abc\r\n\r\n"))
	_, err := ReadFramed(reader)
	if err == nil {
		t.Fatal("expected malformed header error")
	}
	if !strings.Contains(err.Error(), "malformed header") {
		t.Fatalf("expected malformed header error, got %v", err)
	}
}

func TestReadFramed_MissingContentLengthReturnsParseError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Some-Header: value\r\n\r\n"))
	_, err := ReadFramed(reader)
	if err == nil {
		t.Fatal("expected missing Content-Length error")
	}
	if !strings.Contains(err.Error(), "missing Content-Length") {
		t.Fatalf("expected missing Content-Length error, got %v", err)
	}
}

func TestReadFramed_RejectsDuplicateContentLength(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Length: 2\r\nContent-Length: 3\r\n\r\n"))
	_, err := ReadFramed(reader)
	if err == nil {
		t.Fatal("expected duplicate Content-Length error")
	}
	if !strings.Contains(err.Error(), "duplicate Content-Length") {
		t.Fatalf("expected duplicate Content-Length error, got %v", err)
	}
}

func TestDecodeRequest_RejectsWrongVersion(t *testing.T) {
	payload := []byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	_, err := DecodeRequest(payload)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestDecodeRequest_RejectsEmptyMethod(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":""}`)
	_, err := DecodeRequest(payload)
	if err == nil {
		t.Fatal("expected empty method error")
	}
	if !strings.Contains(err.Error(), "method is required") {
		t.Fatalf("expected method is required error, got %v", err)
	}
}

func TestDecodeRequest_IsNotification(t *testing.T) {
	// Notification: no id field.
	notification, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","method":"notify"}`))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if !notification.IsNotification() {
		t.Fatal("expected request with nil id to be a notification")
	}
	// Non-notification: id present.
	request, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if request.IsNotification() {
		t.Fatal("expected request with id not to be a notification")
	}
}

func TestDecodeResponse_RejectsBothResultAndError(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true},"error":{"code":-1,"message":"bad"}}`)
	_, err := DecodeResponse(payload)
	if err == nil {
		t.Fatal("expected result/error exclusivity error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}
