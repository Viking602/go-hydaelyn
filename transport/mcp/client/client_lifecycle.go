package mcpclient

import (
	"context"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Viking602/venat/transport/mcpcontract"
)

// Initialize connects the transport, negotiates the protocol, and sends the
// initialized notification before returning.
func (c *Client) Initialize(ctx context.Context, name, version string) (InitializeResult, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return InitializeResult{}, sdkmcp.ErrConnectionClosed
		}
		if c.transportErr != nil {
			err := c.transportErr
			c.mu.Unlock()
			return InitializeResult{}, err
		}
		if c.initComplete {
			result, err := c.initResult, c.initErr
			c.mu.Unlock()
			return result, err
		}
		if c.initStarted {
			done := c.initDone
			c.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return InitializeResult{}, ctx.Err()
			}
		}

		connectCtx, cancel := context.WithCancel(ctx)
		transport := &trackedTransport{delegate: c.transport}
		c.initStarted = true
		c.initDone = make(chan struct{})
		c.initCancel = cancel
		c.initTransport = transport
		c.mu.Unlock()

		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: name, Version: version}, c.clientOptions())
		session, initErr := client.Connect(connectCtx, transport, nil)
		cancel()
		if initErr != nil {
			c.mu.Lock()
			closed := c.closed
			c.mu.Unlock()
			if !closed {
				initErr = errors.Join(initErr, transport.Close())
			}
			initErr = c.operationError(initErr)
		}

		var result InitializeResult
		if initErr == nil {
			result, initErr = mapInitializeResult(session.InitializeResult())
		}

		c.mu.Lock()
		c.session = session
		c.initResult = result
		c.initErr = initErr
		c.initComplete = true
		c.initCancel = nil
		closed := c.closed
		close(c.initDone)
		c.mu.Unlock()

		if closed {
			return InitializeResult{}, sdkmcp.ErrConnectionClosed
		}
		return result, initErr
	}
}

func (c *Client) clientOptions() *sdkmcp.ClientOptions {
	options := &sdkmcp.ClientOptions{}
	configured := false
	if handler := c.options.ElicitationHandler; handler != nil {
		configured = true
		options.ElicitationHandler = func(ctx context.Context, request *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
			params := request.Params
			result, err := handler(ctx, mcpcontract.Elicitation{
				Mode: params.Mode, Message: params.Message, URL: params.URL,
				ElicitationID: params.ElicitationID, RequestedSchema: params.RequestedSchema,
			})
			if err != nil {
				return nil, err
			}
			return &sdkmcp.ElicitResult{Action: result.Action, Content: result.Content}, nil
		}
	}
	if handler := c.options.NotificationHandler; handler != nil {
		configured = true
		configureNotificationHandlers(options, handler)
	}
	if !configured {
		return nil
	}
	return options
}

func configureNotificationHandlers(options *sdkmcp.ClientOptions, notify mcpcontract.NotificationHandler) {
	options.ToolListChangedHandler = func(ctx context.Context, _ *sdkmcp.ToolListChangedRequest) {
		notify(ctx, mcpcontract.Notification{Kind: "tools/list_changed"})
	}
	options.PromptListChangedHandler = func(ctx context.Context, _ *sdkmcp.PromptListChangedRequest) {
		notify(ctx, mcpcontract.Notification{Kind: "prompts/list_changed"})
	}
	options.ResourceListChangedHandler = func(ctx context.Context, _ *sdkmcp.ResourceListChangedRequest) {
		notify(ctx, mcpcontract.Notification{Kind: "resources/list_changed"})
	}
	options.ResourceUpdatedHandler = func(ctx context.Context, request *sdkmcp.ResourceUpdatedNotificationRequest) {
		notification := mcpcontract.Notification{Kind: "resources/updated"}
		if request != nil && request.Params != nil {
			notification.URI = request.Params.URI
		}
		notify(ctx, notification)
	}
	options.LoggingMessageHandler = func(ctx context.Context, request *sdkmcp.LoggingMessageRequest) {
		notification := mcpcontract.Notification{Kind: "logging/message"}
		if request != nil && request.Params != nil {
			notification.Level = string(request.Params.Level)
			notification.Logger = request.Params.Logger
			notification.Data = request.Params.Data
		}
		notify(ctx, notification)
	}
	options.ProgressNotificationHandler = func(ctx context.Context, request *sdkmcp.ProgressNotificationClientRequest) {
		notification := mcpcontract.Notification{Kind: "progress"}
		if request != nil && request.Params != nil {
			notification.ProgressToken = fmt.Sprint(request.Params.ProgressToken)
			notification.Message = request.Params.Message
			notification.Progress = request.Params.Progress
			notification.Total = request.Params.Total
		}
		notify(ctx, notification)
	}
}

// Close gracefully closes the session. It is idempotent and concurrency safe.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closeDone != nil {
		done := c.closeDone
		c.mu.Unlock()
		<-done
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		return err
	}

	c.closed = true
	c.closeDone = make(chan struct{})
	initDone := c.initDone
	if !c.initStarted || c.initComplete {
		initDone = nil
	}
	cancel := c.initCancel
	stopOperations := c.operationStop
	c.mu.Unlock()

	if stopOperations != nil {
		stopOperations()
	}
	if cancel != nil {
		cancel()
	}
	if initDone != nil {
		<-initDone
	}

	c.mu.Lock()
	transportErr := c.transportErr
	session := c.session
	tracked := c.initTransport
	closer, canCloseTransport := c.transport.(interface{ Close() error })
	c.mu.Unlock()

	var resourceErr error
	if transportErr == nil {
		if session != nil {
			resourceErr = session.Close()
			if canCloseTransport {
				resourceErr = errors.Join(resourceErr, closer.Close())
			}
		} else if tracked != nil {
			resourceErr = tracked.Close()
		} else if canCloseTransport {
			resourceErr = closer.Close()
		}
	}

	c.mu.Lock()
	c.closeErr = errors.Join(transportErr, resourceErr)
	err := c.closeErr
	close(c.closeDone)
	c.mu.Unlock()
	return err
}
