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
	reader    *bufio.Reader
	writer    io.Writer
	closers   []io.Closer
	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
	counter   uint64
}

func NewStreamTransport(reader io.Reader, writer io.Writer, closers ...io.Closer) *StreamTransport {
	return &StreamTransport{
		reader:  bufio.NewReader(reader),
		writer:  writer,
		closers: closers,
	}
}

func (t *StreamTransport) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed.Load() {
		return errStreamTransportClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := atomic.AddUint64(&t.counter, 1)
	request, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return err
	}
	if err := jsonrpc.WriteFramed(t.writer, request); err != nil {
		return err
	}
	responses := make(chan streamResponse, 1)
	go func() {
		responses <- t.readResponse(id)
	}()
	select {
	case response := <-responses:
		if response.err != nil {
			return response.err
		}
		if response.response.Error != nil {
			return fmt.Errorf("%s", response.response.Error.Message)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(response.response.Result, result)
	case <-ctx.Done():
		t.closed.Store(true)
		go func() { _ = t.Close() }()
		return ctx.Err()
	}
}

type streamResponse struct {
	response jsonrpc.Response
	err      error
}

func (t *StreamTransport) readResponse(id uint64) streamResponse {
	for {
		payload, err := jsonrpc.ReadFramed(t.reader)
		if err != nil {
			return streamResponse{err: err}
		}
		response, err := jsonrpc.DecodeResponse(payload)
		if err != nil {
			return streamResponse{err: err}
		}
		if response.ID != nil && fmt.Sprint(response.ID) != fmt.Sprint(id) {
			continue
		}
		return streamResponse{response: response}
	}
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
	})
	return t.closeErr
}

type StdioConfig struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

func DialStdio(ctx context.Context, cfg StdioConfig) (*Client, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Env, cfg.Env...)
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
