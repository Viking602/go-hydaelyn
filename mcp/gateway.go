package mcp

import (
	"context"

	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
	mcpclient "github.com/Viking602/go-hydaelyn/transport/mcp/client"
)

type Gateway interface {
	ImportTools(ctx context.Context) ([]tool.Driver, error)
}

type ClientGateway struct {
	Client *mcpclient.Client
}

func NewGateway(client *mcpclient.Client) ClientGateway {
	return ClientGateway{Client: client}
}

func (g ClientGateway) ImportTools(ctx context.Context) ([]tool.Driver, error) {
	return toolkit.ImportMCPTools(ctx, g.Client)
}
