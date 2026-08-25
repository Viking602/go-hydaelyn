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

		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: name, Version: version}, c.sdkClientOptions())
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

// sdkClientOptions maps registered contract handlers onto SDK client options.
// It returns nil when no handler is registered so the SDK default applies.
func (c *Client) sdkClientOptions() *sdkmcp.ClientOptions {
	clientOptions := &sdkmcp.ClientOptions{}
	hasClientOptions := false
	if c.options.ElicitationHandler != nil {
		hasClientOptions = true
		clientOptions.ElicitationHandler = func(handlerCtx context.Context, request *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
			params := request.Params
			result, handlerErr := c.options.ElicitationHandler(handlerCtx, mcpcontract.Elicitation{
				Mode: params.Mode, Message: params.Message, URL: params.URL,
				ElicitationID: params.ElicitationID, RequestedSchema: params.RequestedSchema,
			})
			if handlerErr != nil {
				return nil, handlerErr
			}
			return &sdkmcp.ElicitResult{Action: result.Action, Content: result.Content}, nil
		}
	}
	if notify := c.options.NotificationHandler; notify != nil {
		hasClientOptions = true
		clientOptions.ToolListChangedHandler = func(handlerCtx context.Context, _ *sdkmcp.ToolListChangedRequest) {
			notify(handlerCtx, mcpcontract.Notification{Kind: "tools/list_changed"})
		}
		clientOptions.PromptListChangedHandler = func(handlerCtx context.Context, _ *sdkmcp.PromptListChangedRequest) {
			notify(handlerCtx, mcpcontract.Notification{Kind: "prompts/list_changed"})
		}
		clientOptions.ResourceListChangedHandler = func(handlerCtx context.Context, _ *sdkmcp.ResourceListChangedRequest) {
			notify(handlerCtx, mcpcontract.Notification{Kind: "resources/list_changed"})
		}
		clientOptions.ResourceUpdatedHandler = func(handlerCtx context.Context, request *sdkmcp.ResourceUpdatedNotificationRequest) {
			notification := mcpcontract.Notification{Kind: "resources/updated"}
			if request != nil && request.Params != nil {
				notification.URI = request.Params.URI
			}
			notify(handlerCtx, notification)
		}
		clientOptions.LoggingMessageHandler = func(handlerCtx context.Context, request *sdkmcp.LoggingMessageRequest) {
			notification := mcpcontract.Notification{Kind: "logging/message"}
			if request != nil && request.Params != nil {
				notification.Level = string(request.Params.Level)
				notification.Logger = request.Params.Logger
				notification.Data = request.Params.Data
			}
			notify(handlerCtx, notification)
		}
		clientOptions.ProgressNotificationHandler = func(handlerCtx context.Context, request *sdkmcp.ProgressNotificationClientRequest) {
			notification := mcpcontract.Notification{Kind: "progress"}
			if request != nil && request.Params != nil {
				notification.ProgressToken = fmt.Sprint(request.Params.ProgressToken)
				notification.Message = request.Params.Message
				notification.Progress = request.Params.Progress
				notification.Total = request.Params.Total
			}
			notify(handlerCtx, notification)
		}
	}
	if !hasClientOptions {
		return nil
	}
	return clientOptions
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
