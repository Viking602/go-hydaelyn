package mcpclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRPCErrorErrorOmitsServerData(t *testing.T) {
	// Given
	err := &RPCError{
		Code:    -32001,
		Message: "tool failed",
		Data:    json.RawMessage(`{"secret":"do-not-log"}`),
	}

	// When
	message := err.Error()

	// Then
	if message != "jsonrpc error -32001: tool failed" {
		t.Fatalf("Error() = %q, want code and message only", message)
	}
	if strings.Contains(message, "do-not-log") {
		t.Fatalf("Error() leaked server data: %q", message)
	}
}
