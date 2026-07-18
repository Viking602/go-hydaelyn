// Package mcpclient implements a typed Model Context Protocol client.
package mcpclient

import (
	"context"
	"errors"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/transport/mcpcontract"
)

// ErrNotInitialized is returned when an operation requires an MCP session
// before [Client.Initialize] has completed.
var ErrNotInitialized = errors.New("mcp client is not initialized")

// ErrInvalidTransport is returned when the client has no usable transport.
var ErrInvalidTransport = errors.New("mcp client transport is invalid")

// Client owns one initialized MCP session.
type Client struct {
	mu            sync.Mutex
	transport     Transport
	transportErr  error
	session       *sdkmcp.ClientSession
	initStarted   bool
	initComplete  bool
	initDone      chan struct{}
	initCancel    context.CancelFunc
	initResult    InitializeResult
	initErr       error
	initTransport *trackedTransport
	closed        bool
	closeDone     chan struct{}
	closeErr      error
	operationCtx  context.Context
	operationStop context.CancelFunc
	options       Options
}

type Options struct {
	ElicitationHandler mcpcontract.ElicitationHandler
}

// New creates a client for transport. Initialize establishes the session.
func New(transport Transport) *Client {
	return NewWithOptions(transport, Options{})
}

// NewWithOptions creates a client with handlers for optional server-to-client
// MCP capabilities.
func NewWithOptions(transport Transport, options Options) *Client {
	operationCtx, operationStop := context.WithCancel(context.Background())
	return &Client{
		transport:     transport,
		transportErr:  validateTransport(transport),
		operationCtx:  operationCtx,
		operationStop: operationStop,
		options:       options,
	}
}

func validateTransport(transport Transport) error {
	if isNilLike(transport) {
		return ErrInvalidTransport
	}
	return nil
}

// ListTools returns one page of server tools.
func (c *Client) ListTools(ctx context.Context) ([]message.ToolDefinition, error) {
	session, err := c.initializedSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, c.operationError(err)
	}
	return mapTools(result.Tools)
}

// CallTool invokes a server tool.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error) {
	session, err := c.initializedSession()
	if err != nil {
		return CallToolResult{}, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallToolResult{}, c.operationError(err)
	}
	return convertJSON[CallToolResult](result)
}

// ListResources returns one page of server resources.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	session, err := c.initializedSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.ListResources(ctx, nil)
	if err != nil {
		return nil, c.operationError(err)
	}
	return mapResources(result.Resources)
}

// ReadResource reads one server resource.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	session, err := c.initializedSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, c.operationError(err)
	}
	return mapResourceContents(result.Contents)
}

// ListPrompts returns one page of server prompts.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	session, err := c.initializedSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.ListPrompts(ctx, nil)
	if err != nil {
		return nil, c.operationError(err)
	}
	return mapPrompts(result.Prompts)
}

// GetPrompt renders a server prompt.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) ([]PromptMessage, error) {
	session, err := c.initializedSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	result, err := session.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, c.operationError(err)
	}
	return mapPromptMessages(result.Messages)
}

func (c *Client) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.operationCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (c *Client) initializedSession() (*sdkmcp.ClientSession, error) {
	if err := c.inboundLimitError(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, sdkmcp.ErrConnectionClosed
	}
	if c.session == nil {
		return nil, ErrNotInitialized
	}
	return c.session, nil
}
