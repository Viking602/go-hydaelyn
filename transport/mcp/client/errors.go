package mcpclient

import (
	"encoding/json"
	"errors"
	"fmt"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// RPCError is returned when the server replies with a JSON-RPC error. Callers
// can use errors.As to inspect both RPCError and the SDK's jsonrpc.Error.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
	cause   error
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

func (e *RPCError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func adaptRPCError(err error) error {
	if err == nil {
		return nil
	}
	var wireErr *sdkjsonrpc.Error
	if !errors.As(err, &wireErr) {
		return err
	}
	return &RPCError{
		Code:    int(wireErr.Code),
		Message: wireErr.Message,
		Data:    json.RawMessage(wireErr.Data),
		cause:   err,
	}
}
