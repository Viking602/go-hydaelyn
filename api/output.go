package api

import "context"

type OutputGateway interface {
	Publish(context.Context, UserMessage) error
}
