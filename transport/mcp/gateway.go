package mcp

import (
	"context"
	"reflect"

	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
	"github.com/Viking602/venat/transport/mcpcontract"
)

// ErrInvalidClient is returned when a gateway has no usable MCP client.
var ErrInvalidClient = kit.ErrInvalidMCPClient

type Gateway interface {
	ImportTools(ctx context.Context) ([]tool.Driver, error)
}

type ClientGateway struct {
	Client mcpcontract.Client
}

func NewGateway(client mcpcontract.Client) ClientGateway {
	if client != nil {
		value := reflect.ValueOf(client)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				client = nil
			}
		}
	}
	return ClientGateway{Client: client}
}

func (g ClientGateway) ImportTools(ctx context.Context) ([]tool.Driver, error) {
	return kit.ImportMCPTools(ctx, g.Client)
}
