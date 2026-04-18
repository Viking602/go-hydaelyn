package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/transport/mcp/jsonrpc"
)

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
		client:  &http.Client{},
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
	defer httpResponse.Body.Close()
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
	reader  *bufio.Reader
	writer  io.Writer
	closers []io.Closer
	mu      sync.Mutex
	counter uint64
}

func NewStreamTransport(reader io.Reader, writer io.Writer, closers ...io.Closer) *StreamTransport {
	return &StreamTransport{
		reader:  bufio.NewReader(reader),
		writer:  writer,
		closers: closers,
	}
}

func (t *StreamTransport) Call(_ context.Context, method string, params any, result any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := atomic.AddUint64(&t.counter, 1)
	request, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return err
	}
	if err := jsonrpc.WriteFramed(t.writer, request); err != nil {
		return err
	}
	for {
		payload, err := jsonrpc.ReadFramed(t.reader)
		if err != nil {
			return err
		}
		response, err := jsonrpc.DecodeResponse(payload)
		if err != nil {
			return err
		}
		if response.ID != nil && fmt.Sprint(response.ID) != fmt.Sprint(id) {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("%s", response.Error.Message)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(response.Result, result)
	}
}

func (t *StreamTransport) Close() error {
	for _, closer := range t.closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
	return nil
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
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	transport := NewStreamTransport(stdout, stdin, stdin, stdout)
	return New(transport), nil
}
