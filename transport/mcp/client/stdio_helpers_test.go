package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func runOfficialStdioServer() {
	defer func() {
		if closeFile := os.Getenv(stdioCloseFileEnv); closeFile != "" {
			_ = os.WriteFile(closeFile, []byte("closed"), 0o600)
		}
	}()
	var initialized atomic.Bool
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "stdio-server", Version: "v1.0.0"},
		&sdkmcp.ServerOptions{InitializedHandler: func(_ context.Context, request *sdkmcp.InitializedRequest) {
			params := request.Session.InitializeParams()
			capabilities := 0
			if params != nil && params.Capabilities != nil {
				payload, _ := json.Marshal(params.Capabilities)
				var values map[string]any
				_ = json.Unmarshal(payload, &values)
				capabilities = len(values)
			}
			workingDirectory, _ := os.Getwd()
			writeJSONFile(os.Getenv(stdioHandshakeFileEnv), stdioHandshake{
				ProtocolVersion:  params.ProtocolVersion,
				Capabilities:     capabilities,
				WorkingDirectory: workingDirectory,
				ParentSecret:     os.Getenv(stdioParentSecretEnv),
				Arguments:        os.Args[1:],
			})
			initialized.Store(true)
		}},
	)
	server.AddTool(&sdkmcp.Tool{Name: "ready", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if !initialized.Load() {
			return nil, &jsonrpc.Error{Code: -32002, Message: "initialized notification missing"}
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "initialized"}}}, nil
	})
	if os.Getenv(stdioOversizeModeEnv) == "1" {
		oversize := strings.Repeat("x", maxInboundMessageBytes+1)
		server.AddTool(&sdkmcp.Tool{Name: "oversize", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: oversize}}}, nil
		})
	}
	_ = server.Run(context.Background(), &sdkmcp.StdioTransport{})
}

func runUnsupportedStdioServer() {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	var request struct {
		ID     any            `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal([]byte(line), &request)
	capabilities, _ := request.Params["capabilities"].(map[string]any)
	writeJSONFile(os.Getenv(stdioHandshakeFileEnv), stdioWireObservation{
		FirstMethod:      request.Method,
		Capabilities:     len(capabilities),
		HadContentLength: len(line) >= len("Content-Length") && line[:len("Content-Length")] == "Content-Length",
	})
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      request.ID,
		"result": map[string]any{
			"protocolVersion": "1900-01-01",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "unsupported", "version": "v1.0.0"},
		},
	})
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = os.WriteFile(os.Getenv(stdioCloseFileEnv), []byte("closed"), 0o600)
}

func runLargeExitStdioServer() {
	reader := bufio.NewReader(os.Stdin)
	initialize := readStdioRequest(reader)
	protocolVersion, _ := initialize.Params["protocolVersion"].(string)
	writeChunkedJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      initialize.ID,
		"result": map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "large-exit", "version": "v1.0.0"},
		},
	})
	for {
		request := readStdioRequest(reader)
		if request.Method != "tools/list" {
			continue
		}
		writeChunkedJSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{
				"tools": []any{map[string]any{
					"name":        "large",
					"description": strings.Repeat("x", stdioLargeResponseLen),
					"inputSchema": map[string]any{"type": "object"},
				}},
			},
		})
		return
	}
}

func runPrettyInitializeStdioServer() {
	initialize := readStdioRequest(bufio.NewReader(os.Stdin))
	protocolVersion, _ := initialize.Params["protocolVersion"].(string)
	payload, _ := json.MarshalIndent(map[string]any{
		"jsonrpc": "2.0",
		"id":      initialize.ID,
		"result": map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "pretty", "version": "v1.0.0"},
		},
	}, "", "  ")
	_, _ = os.Stdout.Write(append(payload, '\n'))
	_, _ = io.Copy(io.Discard, os.Stdin)
}

type stdioRequest struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func readStdioRequest(reader *bufio.Reader) stdioRequest {
	line, _ := reader.ReadBytes('\n')
	var request stdioRequest
	_ = json.Unmarshal(line, &request)
	return request
}

func writeChunkedJSON(value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	payload = append(payload, '\n')
	const chunkSize = 64 << 10
	for len(payload) > 0 {
		end := min(chunkSize, len(payload))
		written, writeErr := os.Stdout.Write(payload[:end])
		if writeErr != nil {
			return
		}
		payload = payload[written:]
	}
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
}

func writeJSONFile(path string, value any) {
	payload, err := json.Marshal(value)
	if err == nil {
		_ = os.WriteFile(path, payload, 0o600)
	}
}
