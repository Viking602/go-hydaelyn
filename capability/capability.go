package capability

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Type string

const (
	TypeLLM         Type = "llm"
	TypeTool        Type = "tool"
	TypeMCP         Type = "mcp"
	TypeSearch      Type = "search"
	TypeRemoteAgent Type = "remote_agent"
)

type Permission struct {
	Name    string `json:"name"`
	Granted bool   `json:"granted,omitempty"`
}

type Budget struct {
	Tokens    int     `json:"tokens,omitempty"`
	ToolCalls int     `json:"toolCalls,omitempty"`
	Cost      float64 `json:"cost,omitempty"`
}

type Usage struct {
	InputTokens  int           `json:"inputTokens,omitempty"`
	OutputTokens int           `json:"outputTokens,omitempty"`
	TotalTokens  int           `json:"totalTokens,omitempty"`
	Cost         float64       `json:"cost,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	StopReason   string        `json:"stopReason,omitempty"`
}

type Call struct {
	Type        Type              `json:"type"`
	Name        string            `json:"name"`
	Input       any               `json:"input,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	MaxRetries  int               `json:"maxRetries,omitempty"`
	Budget      Budget            `json:"budget,omitempty"`
	Permissions []Permission      `json:"permissions,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Result struct {
	Output   any               `json:"output,omitempty"`
	Usage    Usage             `json:"usage,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ErrorKind string

const (
	ErrorKindTimeout    ErrorKind = "timeout"
	ErrorKindPermission ErrorKind = "permission"
	ErrorKindApproval   ErrorKind = "approval"
	ErrorKindRateLimit  ErrorKind = "rate_limit"
	ErrorKindNotFound   ErrorKind = "not_found"
	ErrorKindUpstream   ErrorKind = "upstream"
)

type Error struct {
	Kind      ErrorKind `json:"kind"`
	Message   string    `json:"message"`
	Temporary bool      `json:"temporary,omitempty"`
	Cause     error     `json:"-"`
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

type Handler func(ctx context.Context, call Call) (Result, error)

type Next func(ctx context.Context, call Call) (Result, error)

type Middleware interface {
	Handle(ctx context.Context, call Call, next Next) (Result, error)
}

type Func func(ctx context.Context, call Call, next Next) (Result, error)

func (f Func) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	return f(ctx, call, next)
}

type Invoker struct {
	handlers    map[string]Handler
	middlewares []Middleware
}

func NewInvoker() *Invoker {
	return &Invoker{
		handlers: map[string]Handler{},
	}
}

func (i *Invoker) Register(callType Type, name string, handler Handler) {
	if handler == nil {
		return
	}
	i.handlers[key(callType, name)] = handler
}

func (i *Invoker) Use(middleware Middleware) {
	if middleware == nil {
		return
	}
	i.middlewares = append(i.middlewares, middleware)
}

func (i *Invoker) Invoke(ctx context.Context, call Call) (Result, error) {
	handler, ok := i.handlers[key(call.Type, call.Name)]
	if !ok {
		return Result{}, &Error{Kind: ErrorKindNotFound, Message: fmt.Sprintf("capability handler not found: %s/%s", call.Type, call.Name)}
	}
	if call.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, call.Timeout)
		defer cancel()
	}
	started := time.Now()
	result, err := i.wrap(handler)(ctx, call)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Result{}, &Error{Kind: ErrorKindTimeout, Message: "capability call timed out", Cause: err, Temporary: true}
		}
		var capErr *Error
		if errors.As(err, &capErr) {
			return Result{}, capErr
		}
		return Result{}, &Error{Kind: ErrorKindUpstream, Message: err.Error(), Cause: err, Temporary: true}
	}
	result.Usage.Duration = time.Since(started)
	return result, nil
}

func (i *Invoker) wrap(final Handler) Handler {
	next := func(ctx context.Context, call Call) (Result, error) {
		return final(ctx, call)
	}
	for idx := len(i.middlewares) - 1; idx >= 0; idx-- {
		mw := i.middlewares[idx]
		downstream := next
		next = func(ctx context.Context, call Call) (Result, error) {
			return mw.Handle(ctx, call, downstream)
		}
	}
	return next
}

func Retry(defaultRetries int) Middleware {
	return Func(func(ctx context.Context, call Call, next Next) (Result, error) {
		limit := defaultRetries
		if call.MaxRetries > 0 {
			limit = call.MaxRetries
		}
		var lastErr error
		for attempt := 0; attempt <= limit; attempt++ {
			result, err := next(ctx, call)
			if err == nil {
				return result, nil
			}
			lastErr = err
			var capErr *Error
			if errors.As(err, &capErr) && !capErr.Temporary {
				return Result{}, lastErr
			}
			if attempt < limit {
				backoff := time.Duration(1<<uint(attempt)) * 50 * time.Millisecond
				if backoff > 2*time.Second {
					backoff = 2 * time.Second
				}
				select {
				case <-ctx.Done():
					return Result{}, ctx.Err()
				case <-time.After(backoff):
				}
			}
		}
		return Result{}, lastErr
	})
}

func RequirePermissions() Middleware {
	return Func(func(ctx context.Context, call Call, next Next) (Result, error) {
		for _, permission := range call.Permissions {
			if !permission.Granted {
				return Result{}, &Error{
					Kind:    ErrorKindPermission,
					Message: fmt.Sprintf("permission denied: %s", permission.Name),
				}
			}
		}
		return next(ctx, call)
	})
}

func RequireApproval() Middleware {
	return Func(func(ctx context.Context, call Call, next Next) (Result, error) {
		if call.Metadata != nil && call.Metadata["approved"] == "true" {
			return next(ctx, call)
		}
		return Result{}, &Error{
			Kind:    ErrorKindApproval,
			Message: fmt.Sprintf("approval required for %s/%s", call.Type, call.Name),
		}
	})
}

func RateLimit(limit int) Middleware {
	return RateLimitPerWindow(limit, 0)
}

func RateLimitPerWindow(limit int, window time.Duration) Middleware {
	if limit <= 0 {
		limit = 1
	}
	var mu sync.Mutex
	type entry struct {
		timestamps []time.Time
		total      int
	}
	state := map[string]*entry{}
	return Func(func(ctx context.Context, call Call, next Next) (Result, error) {
		k := key(call.Type, call.Name)
		mu.Lock()
		e := state[k]
		if e == nil {
			e = &entry{}
			state[k] = e
		}
		now := time.Now()
		if window > 0 {
			cutoff := now.Add(-window)
			valid := e.timestamps[:0]
			for _, ts := range e.timestamps {
				if ts.After(cutoff) {
					valid = append(valid, ts)
				}
			}
			e.timestamps = valid
			if len(e.timestamps) >= limit {
				mu.Unlock()
				return Result{}, &Error{
					Kind:      ErrorKindRateLimit,
					Message:   fmt.Sprintf("rate limit exceeded for %s", k),
					Temporary: true,
				}
			}
			e.timestamps = append(e.timestamps, now)
		} else {
			if e.total >= limit {
				mu.Unlock()
				return Result{}, &Error{
					Kind:    ErrorKindRateLimit,
					Message: fmt.Sprintf("rate limit exceeded for %s", k),
				}
			}
			e.total++
		}
		mu.Unlock()
		return next(ctx, call)
	})
}

func BudgetEnforcer() Middleware {
	var mu sync.Mutex
	usage := map[string]*Budget{}
	return Func(func(ctx context.Context, call Call, next Next) (Result, error) {
		k := key(call.Type, call.Name)
		mu.Lock()
		u := usage[k]
		if u == nil {
			u = &Budget{}
			usage[k] = u
		}
		if call.Budget.Tokens > 0 && u.Tokens >= call.Budget.Tokens {
			mu.Unlock()
			return Result{}, &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("token budget exhausted for %s", k)}
		}
		if call.Budget.ToolCalls > 0 && u.ToolCalls >= call.Budget.ToolCalls {
			mu.Unlock()
			return Result{}, &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("tool call budget exhausted for %s", k)}
		}
		if call.Budget.Cost > 0 && u.Cost >= call.Budget.Cost {
			mu.Unlock()
			return Result{}, &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("cost budget exhausted for %s", k)}
		}
		mu.Unlock()
		result, err := next(ctx, call)
		if err != nil {
			return result, err
		}
		mu.Lock()
		u.Tokens += result.Usage.TotalTokens
		u.ToolCalls++
		u.Cost += result.Usage.Cost
		mu.Unlock()
		return result, nil
	})
}

func key(callType Type, name string) string {
	return string(callType) + "/" + name
}
