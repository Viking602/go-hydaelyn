package errorprovider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
)

type Kind string

const (
	KindTimeout       Kind = "timeout"
	KindRateLimit     Kind = "rate_limit"
	KindUpstreamError Kind = "upstream_error"
)

var (
	ErrRateLimit = errors.New("provider rate limit")
	ErrUpstream  = errors.New("provider upstream error")
)

type ErrorProvider struct {
	Failure Kind
	Delay   time.Duration
	Meta    provider.Metadata
}

func New(failure Kind) *ErrorProvider {
	return &ErrorProvider{
		Failure: failure,
		Meta:    provider.Metadata{Name: "error", Models: []string{"error"}},
	}
}

func (p *ErrorProvider) Metadata() provider.Metadata {
	if p.Meta.Name == "" {
		return provider.Metadata{Name: "error", Models: []string{"error"}}
	}
	return p.Meta
}

func (p *ErrorProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	switch p.Failure {
	case KindTimeout:
		delay := p.Delay
		if delay <= 0 {
			delay = 10 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, context.DeadlineExceeded
		}
	case KindRateLimit:
		return nil, ErrRateLimit
	case KindUpstreamError:
		return nil, ErrUpstream
	default:
		return nil, fmt.Errorf("unsupported error provider kind %q", p.Failure)
	}
}
