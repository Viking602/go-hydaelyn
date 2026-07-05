package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	Version       = "2.0"
	MaxFrameBytes = 4 << 20
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func NewRequest(id any, method string, params any) (Request, error) {
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return Request{}, err
		}
		raw = encoded
	}
	return Request{
		JSONRPC: Version,
		ID:      id,
		Method:  method,
		Params:  raw,
	}, nil
}

func Success(id any, result any) (Response, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return Response{}, err
	}
	return Response{
		JSONRPC: Version,
		ID:      id,
		Result:  encoded,
	}, nil
}

func Failure(id any, code int, message string, data any) Response {
	return Response{
		JSONRPC: Version,
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

func DecodeRequest(payload []byte) (Request, error) {
	var request Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return Request{}, err
	}
	if request.JSONRPC == "" {
		request.JSONRPC = Version
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func DecodeResponse(payload []byte) (Response, error) {
	var response Response
	if err := json.Unmarshal(payload, &response); err != nil {
		return Response{}, err
	}
	if response.JSONRPC == "" {
		response.JSONRPC = Version
	}
	if err := response.Validate(); err != nil {
		return Response{}, err
	}
	return response, nil
}

// Validate enforces JSON-RPC 2.0 request rules.
func (r Request) Validate() error {
	if r.JSONRPC != Version {
		return fmt.Errorf("jsonrpc: invalid version %q, expected %q", r.JSONRPC, Version)
	}
	if r.Method == "" {
		return fmt.Errorf("jsonrpc: method is required")
	}
	return nil
}

// IsNotification reports whether the request is a notification (no id).
func (r Request) IsNotification() bool {
	return r.ID == nil
}

// Validate enforces JSON-RPC 2.0 response rules.
func (r Response) Validate() error {
	if r.JSONRPC != Version {
		return fmt.Errorf("jsonrpc: invalid version %q, expected %q", r.JSONRPC, Version)
	}
	if r.Result != nil && r.Error != nil {
		return fmt.Errorf("jsonrpc: result and error are mutually exclusive")
	}
	return nil
}

func ReadFramed(reader *bufio.Reader) ([]byte, error) {
	length := 0
	seenLength := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("jsonrpc: malformed header %q", line)
		}
		if strings.EqualFold(parts[0], "Content-Length") {
			if seenLength {
				return nil, fmt.Errorf("jsonrpc: duplicate Content-Length header")
			}
			value := strings.TrimSpace(parts[1])
			length, err = strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			seenLength = true
		}
	}
	if !seenLength {
		return nil, fmt.Errorf("jsonrpc: missing Content-Length")
	}
	if length <= 0 {
		return nil, fmt.Errorf("jsonrpc: invalid Content-Length %d", length)
	}
	if length > MaxFrameBytes {
		return nil, fmt.Errorf("jsonrpc: frame too large: content-length %d exceeds %d", length, MaxFrameBytes)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func WriteFramed(writer io.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.Copy(writer, bytes.NewBufferString(header)); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
