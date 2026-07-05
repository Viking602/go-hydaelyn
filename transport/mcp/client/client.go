// Package mcpclient implements a Model Context Protocol (MCP) client over
// JSON-RPC, supporting both HTTP and stdio transports for talking to MCP
// servers. Although the package lives at transport/mcp/client/, the package
// name is `mcpclient` because `client` is too generic to be useful in error
// messages, stack traces, or unaliased imports.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/transport/mcp/jsonrpc"
)

const (
	defaultHTTPTransportTimeout = 30 * time.Second
	defaultStdioCloseTimeout    = time.Second
)

var errStreamTransportClosed = errors.New("mcp stream transport closed")

type Transport interface {
	Call(ctx context.Context, method string, params any, result any) error
	Close() error
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ServerInfo      ServerInfo     `json:"serverInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content           []ContentBlock `json:"content"`
	IsError           bool           `json:"isError,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
}

type Client struct {
	transport Transport
}

func New(transport Transport) *Client {
	return &Client{transport: transport}
}

func (c *Client) Close() error {
	if c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

func (c *Client) Initialize(ctx context.Context, name, version string) (InitializeResult, error) {
	result := InitializeResult{}
	err := c.transport.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo": map[string]any{
			"name":    name,
			"version": version,
		},
	}, &result)
	return result, err
}

func (c *Client) ListTools(ctx context.Context) ([]message.ToolDefinition, error) {
	var result struct {
		Tools []message.ToolDefinition `json:"tools"`
	}
	if err := c.transport.Call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error) {
	result := CallToolResult{}
	err := c.transport.Call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	}, &result)
	return result, err
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var result struct {
		Resources []Resource `json:"resources"`
	}
	if err := c.transport.Call(ctx, "resources/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	var result struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := c.transport.Call(ctx, "resources/read", map[string]any{"uri": uri}, &result); err != nil {
		return nil, err
	}
	return result.Contents, nil
}

func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var result struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := c.transport.Call(ctx, "prompts/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) ([]PromptMessage, error) {
	var result struct {
		Messages []PromptMessage `json:"messages"`
	}
	params := map[string]any{"name": name}
	if len(arguments) > 0 {
		params["arguments"] = arguments
	}
	if err := c.transport.Call(ctx, "prompts/get", params, &result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

type HTTPTransport struct {
	client  *http.Client
	url     string
	headers http.Header
	counter uint64
}

func NewHTTPTransport(url string, headers http.Header) *HTTPTransport {
	cloned := http.Header{}
	if headers != nil {
		cloned = headers.Clone()
	}
	return &HTTPTransport{
		client:  &http.Client{Timeout: defaultHTTPTransportTimeout},
		url:     url,
		headers: cloned,
	}
}

func (t *HTTPTransport) Call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddUint64(&t.counter, 1)
	request, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpRequest.Header = t.headers.Clone()
	httpRequest.Header.Set("Content-Type", "application/json")
	httpResponse, err := t.client.Do(httpRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResponse.Body.Close() }()
	var response jsonrpc.Response
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("%s", response.Error.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(response.Result, result)
}

func (t *HTTPTransport) Close() error {
	return nil
}

type StreamTransport struct {
	reader     *bufio.Reader
	writer     io.Writer
	closers    []io.Closer
	writeMu    sync.Mutex // serializes writes to the shared writer
	callsMu    sync.Mutex // guards calls map
	calls      map[uint64]chan streamResponse
	readerOnce sync.Once     // starts the long-lived reader goroutine lazily
	readerDone chan struct{} // closed when the reader goroutine exits
	closeOnce  sync.Once
	closeErr   error
	closed     atomic.Bool
	counter    uint64
}

func NewStreamTransport(reader io.Reader, writer io.Writer, closers ...io.Closer) *StreamTransport {
	return &StreamTransport{
		reader:  bufio.NewReader(reader),
		writer:  writer,
		closers: closers,
		calls:   make(map[uint64]chan streamResponse),
	}
}

// RPCError is the typed error returned by Call when the server replies with a
// JSON-RPC error response. Callers can use [errors.As] to extract the code.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

func (t *StreamTransport) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.closed.Load() {
		return errStreamTransportClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	t.startReader()

	id := atomic.AddUint64(&t.counter, 1)
	request, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return err
	}

	responseCh := make(chan streamResponse, 1)
	t.callsMu.Lock()
	if t.closed.Load() {
		t.callsMu.Unlock()
		return errStreamTransportClosed
	}
	t.calls[id] = responseCh
	t.callsMu.Unlock()

	t.writeMu.Lock()
	err = jsonrpc.WriteFramed(t.writer, request)
	t.writeMu.Unlock()
	if err != nil {
		t.unregisterCall(id)
		return err
	}

	select {
	case response := <-responseCh:
		if response.err != nil {
			return response.err
		}
		if response.response.Error != nil {
			rpcErr := &RPCError{
				Code:    response.response.Error.Code,
				Message: response.response.Error.Message,
			}
			if response.response.Error.Data != nil {
				if raw, mErr := json.Marshal(response.response.Error.Data); mErr == nil {
					rpcErr.Data = raw
				}
			}
			return rpcErr
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(response.response.Result, result)
	case <-ctx.Done():
		t.unregisterCall(id)
		return ctx.Err()
	}
}

// unregisterCall removes a pending call from the registry. Safe to call when
// the call was already dispatched or never registered.
func (t *StreamTransport) unregisterCall(id uint64) {
	t.callsMu.Lock()
	delete(t.calls, id)
	t.callsMu.Unlock()
}

// startReader launches the single long-lived reader goroutine the first time
// Call is invoked. It is a no-op on subsequent calls.
func (t *StreamTransport) startReader() {
	t.readerOnce.Do(func() {
		t.readerDone = make(chan struct{})
		go t.readLoop()
	})
}

// readLoop runs in its own goroutine, reads framed messages, and routes them
// to the per-call channels registered in t.calls. Notifications (id == nil) and
// responses for ids with no waiting caller are dropped. The loop exits when the
// reader returns an error (e.g. EOF or the transport being closed).
func (t *StreamTransport) readLoop() {
	defer close(t.readerDone)
	for {
		payload, err := jsonrpc.ReadFramed(t.reader)
		if err != nil {
			t.closed.Store(true)
			t.failPending(err)
			return
		}
		response, err := jsonrpc.DecodeResponse(payload)
		if err != nil {
			t.closed.Store(true)
			t.failPending(err)
			return
		}
		// Notifications (no id) must never satisfy a waiting Call.
		if response.ID == nil {
			continue
		}
		id, ok := normalizeResponseID(response.ID)
		if !ok {
			continue
		}
		t.callsMu.Lock()
		ch, ok := t.calls[id]
		if ok {
			delete(t.calls, id)
		}
		t.callsMu.Unlock()
		if !ok {
			// No caller is waiting for this id; drop it.
			continue
		}
		select {
		case ch <- streamResponse{response: response}:
		default:
			// Caller already abandoned (ctx canceled) and unregistered; drop.
		}
	}
}

// failPending delivers a terminal error to every waiting caller and clears the
// registry. Used when the reader goroutine ends (EOF, read error, or Close).
func (t *StreamTransport) failPending(err error) {
	t.callsMu.Lock()
	defer t.callsMu.Unlock()
	for id, ch := range t.calls {
		select {
		case ch <- streamResponse{err: err}:
		default:
		}
		delete(t.calls, id)
	}
}

// normalizeResponseID converts a JSON-RPC response id (which may arrive as a
// number or string depending on the server) to the uint64 key used by Call.
// Returns ok=false for ids that cannot be matched to a waiting call.
func normalizeResponseID(id any) (uint64, bool) {
	switch v := id.(type) {
	case float64:
		if v != float64(uint64(v)) {
			return 0, false
		}
		return uint64(v), true
	case int:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case uint64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil || n < 0 {
			return 0, false
		}
		return uint64(n), true
	case string:
		// String ids are server-issued and never match our uint64 counter;
		// reject so they cannot be confused with a waiting call.
		return 0, false
	default:
		return 0, false
	}
}

type streamResponse struct {
	response jsonrpc.Response
	err      error
}

func (t *StreamTransport) Close() error {
	t.closeOnce.Do(func() {
		t.closed.Store(true)
		for _, closer := range t.closers {
			if closer == nil {
				continue
			}
			if err := closer.Close(); err != nil && t.closeErr == nil {
				t.closeErr = err
			}
		}
		t.callsMu.Lock()
		// Fail any caller still blocked in Call so it doesn't wait forever.
		for id, ch := range t.calls {
			select {
			case ch <- streamResponse{err: errStreamTransportClosed}:
			default:
			}
			delete(t.calls, id)
		}
		t.callsMu.Unlock()
	})
	return t.closeErr
}

// StdioConfig describes an MCP server process launched over stdin/stdout.
type StdioConfig struct {
	Command string
	Args    []string
	Dir     string
	// Env is the complete child process environment. By default it replaces the
	// parent environment even when empty, so untrusted stdio servers do not
	// receive ambient API keys or credentials.
	Env []string
	// InheritEnv prepends os.Environ() before Env. Use this only for trusted
	// subprocesses that need additive environment overrides.
	InheritEnv bool
}

func DialStdio(ctx context.Context, cfg StdioConfig) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	if cfg.InheritEnv {
		cmd.Env = append(os.Environ(), cfg.Env...)
	} else {
		cmd.Env = append([]string{}, cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	transport := NewStreamTransport(stdout, stdin, newStdioProcess(cmd, stdin, stdout))
	return New(transport), nil
}

type stdioProcess struct {
	cmd      *exec.Cmd
	stdin    io.Closer
	stdout   io.Closer
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func newStdioProcess(cmd *exec.Cmd, stdin io.Closer, stdout io.Closer) *stdioProcess {
	return &stdioProcess{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		waitDone: make(chan struct{}),
	}
}

func (p *stdioProcess) Close() error {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	go p.wait()
	select {
	case <-p.waitDone:
		return nil
	case <-time.After(defaultStdioCloseTimeout):
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.waitDone
		return nil
	}
}

func (p *stdioProcess) wait() {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.waitDone)
	})
}
