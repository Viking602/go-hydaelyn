package capability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
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
	Tokens    int         `json:"tokens,omitempty"`
	ToolCalls int         `json:"toolCalls,omitempty"`
	Cost      float64     `json:"cost,omitempty"`
	Scope     BudgetScope `json:"scope,omitempty"`
}

type BudgetScope string

const (
	BudgetScopeCall      BudgetScope = "call"
	BudgetScopeRun       BudgetScope = "run"
	BudgetScopeTeam      BudgetScope = "team"
	BudgetScopePrincipal BudgetScope = "principal"
	BudgetScopeGlobal    BudgetScope = "global"
)

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
	ErrorKindStaleWrite ErrorKind = "stale_write"
	ErrorKindNotFound   ErrorKind = "not_found"
	ErrorKindUpstream   ErrorKind = "upstream"
)

const (
	policyCapabilityPermission = "capability.permission"
	policyCapabilityApproval   = "capability.approval"
	policyCapabilityRetry      = "capability.retry"
	policyCapabilityTimeout    = "capability.timeout"
	policyCapabilityRateLimit  = "capability.rate_limit"
	policyCapabilityStaleWrite = "capability.stale_write"
)

type PolicyOutcomeRecorder interface {
	RecordPolicyOutcome(ctx context.Context, event storage.PolicyOutcomeEvent)
}

type policyOutcomeRecorderKey struct{}

func WithPolicyOutcomeRecorder(ctx context.Context, recorder PolicyOutcomeRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, policyOutcomeRecorderKey{}, recorder)
}

func WithApproval(ctx context.Context, callType Type, name string) context.Context {
	return WithApprovalGrant(ctx, ApprovalGrant{Type: callType, Name: name})
}

func hasApproval(ctx context.Context, call Call) bool {
	security, ok := SecurityContextFromContext(ctx)
	if !ok || len(security.ApprovalGrants) == 0 {
		return false
	}
	_, ok = security.ApprovalGrants[key(call.Type, call.Name)]
	return ok
}

func emitPolicyOutcome(ctx context.Context, event storage.PolicyOutcomeEvent) {
	recorder, _ := ctx.Value(policyOutcomeRecorderKey{}).(PolicyOutcomeRecorder)
	if recorder == nil {
		return
	}
	if event.SchemaVersion == "" {
		event.SchemaVersion = storage.PolicyOutcomeEventSchemaVersion
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	recorder.RecordPolicyOutcome(ctx, event)
}

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
type StreamHandler func(ctx context.Context, call Call) (provider.Stream, error)

type Next func(ctx context.Context, call Call) (Result, error)
type StreamNext func(ctx context.Context, call Call) (provider.Stream, error)

type Middleware interface {
	Handle(ctx context.Context, call Call, next Next) (Result, error)
}

type StreamMiddleware interface {
	HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error)
}

type Policy = Middleware
type PolicyFunc = Func

type Func func(ctx context.Context, call Call, next Next) (Result, error)

func (f Func) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	return f(ctx, call, next)
}

type Invoker struct {
	handlers       map[string]Handler
	streamHandlers map[string]StreamHandler
	middlewares    []Middleware
}

func NewInvoker() *Invoker {
	return &Invoker{
		handlers:       map[string]Handler{},
		streamHandlers: map[string]StreamHandler{},
	}
}

func (i *Invoker) Register(callType Type, name string, handler Handler) {
	if handler == nil {
		return
	}
	i.handlers[key(callType, name)] = handler
}

func (i *Invoker) RegisterStream(callType Type, name string, handler StreamHandler) {
	if handler == nil {
		return
	}
	i.streamHandlers[key(callType, name)] = handler
}

// Deprecated: use UsePolicy instead.
func (i *Invoker) Use(middleware Middleware) {
	if middleware == nil {
		return
	}
	i.middlewares = append(i.middlewares, middleware)
}

func (i *Invoker) UsePolicy(policy Policy) {
	i.Use(policy)
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
			emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityTimeout, "timed_out", "critical", "capability call timed out", true, 0, map[string]string{"errorKind": string(ErrorKindTimeout)}))
			return Result{}, &Error{Kind: ErrorKindTimeout, Message: "capability call timed out", Cause: err, Temporary: true}
		}
		var capErr *Error
		if errors.As(err, &capErr) {
			if capErr.Kind == ErrorKindStaleWrite {
				emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityStaleWrite, "rejected", "error", capErr.Message, true, 0, map[string]string{"errorKind": string(capErr.Kind)}))
			}
			return Result{}, capErr
		}
		return Result{}, &Error{Kind: ErrorKindUpstream, Message: err.Error(), Cause: err, Temporary: true}
	}
	result.Usage.Duration = time.Since(started)
	return result, nil
}

func (i *Invoker) Stream(ctx context.Context, call Call) (provider.Stream, error) {
	handler, ok := i.streamHandlers[key(call.Type, call.Name)]
	if !ok {
		return nil, &Error{Kind: ErrorKindNotFound, Message: fmt.Sprintf("capability stream handler not found: %s/%s", call.Type, call.Name)}
	}
	if call.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, call.Timeout)
		stream, err := i.wrapStream(handler)(ctx, call)
		if err != nil {
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityTimeout, "timed_out", "critical", "capability call timed out", true, 0, map[string]string{"errorKind": string(ErrorKindTimeout)}))
				return nil, &Error{Kind: ErrorKindTimeout, Message: "capability call timed out", Cause: err, Temporary: true}
			}
			var capErr *Error
			if errors.As(err, &capErr) {
				if capErr.Kind == ErrorKindStaleWrite {
					emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityStaleWrite, "rejected", "error", capErr.Message, true, 0, map[string]string{"errorKind": string(capErr.Kind)}))
				}
				return nil, capErr
			}
			return nil, &Error{Kind: ErrorKindUpstream, Message: err.Error(), Cause: err, Temporary: true}
		}
		return &streamCancelWrapper{Stream: stream, cancel: cancel}, nil
	}
	stream, err := i.wrapStream(handler)(ctx, call)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityTimeout, "timed_out", "critical", "capability call timed out", true, 0, map[string]string{"errorKind": string(ErrorKindTimeout)}))
			return nil, &Error{Kind: ErrorKindTimeout, Message: "capability call timed out", Cause: err, Temporary: true}
		}
		var capErr *Error
		if errors.As(err, &capErr) {
			if capErr.Kind == ErrorKindStaleWrite {
				emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityStaleWrite, "rejected", "error", capErr.Message, true, 0, map[string]string{"errorKind": string(capErr.Kind)}))
			}
			return nil, capErr
		}
		return nil, &Error{Kind: ErrorKindUpstream, Message: err.Error(), Cause: err, Temporary: true}
	}
	return stream, nil
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

func (i *Invoker) wrapStream(final StreamHandler) StreamHandler {
	next := func(ctx context.Context, call Call) (provider.Stream, error) {
		return final(ctx, call)
	}
	for idx := len(i.middlewares) - 1; idx >= 0; idx-- {
		mw := i.middlewares[idx]
		downstream := next
		if streamMW, ok := mw.(StreamMiddleware); ok {
			next = func(ctx context.Context, call Call) (provider.Stream, error) {
				return streamMW.HandleStream(ctx, call, downstream)
			}
			continue
		}
		next = func(ctx context.Context, call Call) (provider.Stream, error) {
			var stream provider.Stream
			_, err := mw.Handle(ctx, call, func(ctx context.Context, call Call) (Result, error) {
				current, err := downstream(ctx, call)
				if err != nil {
					return Result{}, err
				}
				stream = current
				return Result{}, nil
			})
			if err != nil {
				return nil, err
			}
			return stream, nil
		}
	}
	return next
}

func Retry(defaultRetries int) Middleware {
	return retryMiddleware{defaultRetries: defaultRetries}
}

func RequirePermissions() Middleware {
	return requirePermissionsMiddleware{}
}

func RequireApproval() Middleware {
	return requireApprovalMiddleware{}
}

func RateLimit(limit int) Middleware {
	return RateLimitPerWindow(limit, 0)
}

func RateLimitPerWindow(limit int, window time.Duration) Middleware {
	if limit <= 0 {
		limit = 1
	}
	var mu sync.Mutex
	state := map[string]*rateLimitEntry{}
	return rateLimitMiddleware{
		limit:  limit,
		window: window,
		state:  state,
		mu:     &mu,
	}
}

func BudgetEnforcer() Middleware {
	return &budgetEnforcerMiddleware{
		usage: map[string]*Budget{},
	}
}

type retryMiddleware struct {
	defaultRetries int
}

func (m retryMiddleware) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	limit := retryLimit(call, m.defaultRetries)
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
			if err := waitRetryAttempt(ctx, call, attempt, err); err != nil {
				return Result{}, err
			}
		}
	}
	return Result{}, lastErr
}

func (m retryMiddleware) HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error) {
	limit := retryLimit(call, m.defaultRetries)
	var lastErr error
	for attempt := 0; attempt <= limit; attempt++ {
		stream, err := next(ctx, call)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		var capErr *Error
		if errors.As(err, &capErr) && !capErr.Temporary {
			return nil, lastErr
		}
		if attempt < limit {
			if err := waitRetryAttempt(ctx, call, attempt, err); err != nil {
				return nil, err
			}
		}
	}
	return nil, lastErr
}

type requirePermissionsMiddleware struct{}

func (requirePermissionsMiddleware) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	if err := validatePermissions(ctx, call); err != nil {
		return Result{}, err
	}
	return next(ctx, call)
}

func (requirePermissionsMiddleware) HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error) {
	if err := validatePermissions(ctx, call); err != nil {
		return nil, err
	}
	return next(ctx, call)
}

type requireApprovalMiddleware struct{}

func (requireApprovalMiddleware) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	if err := validateApproval(ctx, call); err != nil {
		return Result{}, err
	}
	return next(ctx, call)
}

func (requireApprovalMiddleware) HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error) {
	if err := validateApproval(ctx, call); err != nil {
		return nil, err
	}
	return next(ctx, call)
}

type rateLimitMiddleware struct {
	limit  int
	window time.Duration
	state  map[string]*rateLimitEntry
	mu     *sync.Mutex
}

type rateLimitEntry struct {
	timestamps []time.Time
	total      int
}

func (m rateLimitMiddleware) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	if err := m.allow(ctx, call); err != nil {
		return Result{}, err
	}
	return next(ctx, call)
}

func (m rateLimitMiddleware) HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error) {
	if err := m.allow(ctx, call); err != nil {
		return nil, err
	}
	return next(ctx, call)
}

func (m rateLimitMiddleware) allow(ctx context.Context, call Call) error {
	k := key(call.Type, call.Name)
	m.mu.Lock()
	e := m.state[k]
	if e == nil {
		e = &rateLimitEntry{}
		m.state[k] = e
	}
	now := time.Now()
	if m.window > 0 {
		cutoff := now.Add(-m.window)
		valid := e.timestamps[:0]
		for _, ts := range e.timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		e.timestamps = valid
		if len(e.timestamps) >= m.limit {
			m.mu.Unlock()
			emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRateLimit, "rate_limited", "warning", fmt.Sprintf("rate limit exceeded for %s", k), true, 0, map[string]string{"errorKind": string(ErrorKindRateLimit)}))
			return &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("rate limit exceeded for %s", k), Temporary: true}
		}
		e.timestamps = append(e.timestamps, now)
		m.mu.Unlock()
		return nil
	}
	if e.total >= m.limit {
		m.mu.Unlock()
		emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRateLimit, "rate_limited", "warning", fmt.Sprintf("rate limit exceeded for %s", k), true, 0, map[string]string{"errorKind": string(ErrorKindRateLimit)}))
		return &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("rate limit exceeded for %s", k)}
	}
	e.total++
	m.mu.Unlock()
	return nil
}

type budgetEnforcerMiddleware struct {
	mu    sync.Mutex
	usage map[string]*Budget
}

func (m *budgetEnforcerMiddleware) Handle(ctx context.Context, call Call, next Next) (Result, error) {
	k, err := m.requireBudgetCapacity(ctx, call)
	if err != nil {
		return Result{}, err
	}
	result, err := next(ctx, call)
	if err != nil {
		return result, err
	}
	m.consumeBudget(k, result.Usage)
	return result, nil
}

func (m *budgetEnforcerMiddleware) HandleStream(ctx context.Context, call Call, next StreamNext) (provider.Stream, error) {
	k, err := m.requireBudgetCapacity(ctx, call)
	if err != nil {
		return nil, err
	}
	stream, err := next(ctx, call)
	if err != nil {
		return nil, err
	}
	var usage Usage
	completed := false
	return newTappedStream(stream, func(event provider.Event) {
		if event.Kind == provider.EventDone {
			completed = true
			usage = Usage{
				InputTokens:  event.Usage.InputTokens,
				OutputTokens: event.Usage.OutputTokens,
				TotalTokens:  event.Usage.TotalTokens,
				Cost:         usage.Cost,
				StopReason:   string(event.StopReason),
			}
			return
		}
		usage.InputTokens += event.Usage.InputTokens
		usage.OutputTokens += event.Usage.OutputTokens
		usage.TotalTokens += event.Usage.TotalTokens
	}, func(err error) {
		if err == nil || errors.Is(err, io.EOF) || completed {
			m.consumeBudget(k, usage)
		}
	}), nil
}

func (m *budgetEnforcerMiddleware) requireBudgetCapacity(ctx context.Context, call Call) (string, error) {
	k := budgetKey(ctx, call)
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.usage[k]
	if u == nil {
		u = &Budget{}
		m.usage[k] = u
	}
	if call.Budget.Tokens > 0 && u.Tokens >= call.Budget.Tokens {
		emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRateLimit, "rate_limited", "warning", fmt.Sprintf("token budget exhausted for %s", k), true, 0, map[string]string{"budget": "tokens", "errorKind": string(ErrorKindRateLimit), "budgetScope": string(normalizeBudgetScope(call.Budget.Scope))}))
		return "", &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("token budget exhausted for %s", k)}
	}
	if call.Budget.ToolCalls > 0 && u.ToolCalls >= call.Budget.ToolCalls {
		emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRateLimit, "rate_limited", "warning", fmt.Sprintf("tool call budget exhausted for %s", k), true, 0, map[string]string{"budget": "tool_calls", "errorKind": string(ErrorKindRateLimit), "budgetScope": string(normalizeBudgetScope(call.Budget.Scope))}))
		return "", &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("tool call budget exhausted for %s", k)}
	}
	if call.Budget.Cost > 0 && u.Cost >= call.Budget.Cost {
		emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRateLimit, "rate_limited", "warning", fmt.Sprintf("cost budget exhausted for %s", k), true, 0, map[string]string{"budget": "cost", "errorKind": string(ErrorKindRateLimit), "budgetScope": string(normalizeBudgetScope(call.Budget.Scope))}))
		return "", &Error{Kind: ErrorKindRateLimit, Message: fmt.Sprintf("cost budget exhausted for %s", k)}
	}
	return k, nil
}

func (m *budgetEnforcerMiddleware) consumeBudget(k string, usage Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.usage[k]
	if current == nil {
		current = &Budget{}
		m.usage[k] = current
	}
	current.Tokens += usage.TotalTokens
	current.ToolCalls++
	current.Cost += usage.Cost
}

func key(callType Type, name string) string {
	return string(callType) + "/" + name
}

func retryLimit(call Call, defaultRetries int) int {
	if call.MaxRetries > 0 {
		return call.MaxRetries
	}
	return defaultRetries
}

func waitRetryAttempt(ctx context.Context, call Call, attempt int, err error) error {
	emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityRetry, "retrying", "info", fmt.Sprintf("retry attempt %d for %s after %v", attempt+1, key(call.Type, call.Name), err), false, attempt+1, nil))
	backoff := time.Duration(1<<uint(attempt)) * 50 * time.Millisecond
	if backoff > 2*time.Second {
		backoff = 2 * time.Second
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}

func validatePermissions(ctx context.Context, call Call) error {
	for _, permission := range call.Permissions {
		if !hasPermissionGrant(ctx, permission.Name) {
			emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityPermission, "denied", "error", fmt.Sprintf("permission denied: %s", permission.Name), true, 0, map[string]string{"permission": permission.Name, "errorKind": string(ErrorKindPermission)}))
			return &Error{Kind: ErrorKindPermission, Message: fmt.Sprintf("permission denied: %s", permission.Name)}
		}
	}
	return nil
}

func validateApproval(ctx context.Context, call Call) error {
	if hasApproval(ctx, call) {
		return nil
	}
	emitPolicyOutcome(ctx, newPolicyOutcomeEvent(call, policyCapabilityApproval, "paused", "warning", fmt.Sprintf("approval required for %s/%s", call.Type, call.Name), true, 0, map[string]string{"errorKind": string(ErrorKindApproval)}))
	return &Error{Kind: ErrorKindApproval, Message: fmt.Sprintf("approval required for %s/%s", call.Type, call.Name)}
}

func budgetKey(ctx context.Context, call Call) string {
	scope := normalizeBudgetScope(call.Budget.Scope)
	base := key(call.Type, call.Name)
	switch scope {
	case BudgetScopeCall:
		if value := firstNonEmpty(call.Metadata["callId"], call.Metadata["requestId"], call.Metadata["idempotencyKey"]); value != "" {
			return base + "/call/" + value
		}
	case BudgetScopeRun:
		if value := firstNonEmpty(call.Metadata["runId"], call.Metadata["sessionId"]); value != "" {
			return base + "/run/" + value
		}
	case BudgetScopeTeam:
		if value := call.Metadata["teamId"]; value != "" {
			return base + "/team/" + value
		}
	case BudgetScopePrincipal:
		if security, ok := SecurityContextFromContext(ctx); ok && security.Principal != "" {
			return base + "/principal/" + security.Principal
		}
	case BudgetScopeGlobal:
		return base
	}
	return base
}

func normalizeBudgetScope(scope BudgetScope) BudgetScope {
	if scope == "" {
		return BudgetScopeRun
	}
	return scope
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type tappedStream struct {
	inner   provider.Stream
	onEvent func(provider.Event)
	onClose func(error)
	once    sync.Once
}

func newTappedStream(inner provider.Stream, onEvent func(provider.Event), onClose func(error)) provider.Stream {
	return &tappedStream{inner: inner, onEvent: onEvent, onClose: onClose}
}

func (s *tappedStream) Recv() (provider.Event, error) {
	event, err := s.inner.Recv()
	if err == nil && s.onEvent != nil {
		s.onEvent(event)
		return event, nil
	}
	if err != nil {
		s.finish(err)
	}
	return event, err
}

func (s *tappedStream) Close() error {
	err := s.inner.Close()
	s.finish(err)
	return err
}

func (s *tappedStream) finish(err error) {
	s.once.Do(func() {
		if s.onClose != nil {
			s.onClose(err)
		}
	})
}

type streamCancelWrapper struct {
	provider.Stream
	cancel context.CancelFunc
}

func (s *streamCancelWrapper) Close() error {
	err := s.Stream.Close()
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

func newPolicyOutcomeEvent(call Call, policy, outcome, severity, message string, blocking bool, attempt int, metadata map[string]string) storage.PolicyOutcomeEvent {
	evidenceMetadata := cloneMetadata(call.Metadata)
	if evidenceMetadata == nil {
		evidenceMetadata = map[string]string{}
	}
	evidenceMetadata["capabilityType"] = string(call.Type)
	evidenceMetadata["capabilityName"] = call.Name
	for key, value := range metadata {
		evidenceMetadata[key] = value
	}
	event := storage.PolicyOutcomeEvent{
		SchemaVersion: storage.PolicyOutcomeEventSchemaVersion,
		Layer:         "capability",
		Stage:         string(call.Type),
		Operation:     capabilityPolicyOperation(outcome),
		Action:        capabilityPolicyAction(outcome),
		Policy:        policy,
		Outcome:       outcome,
		Severity:      severity,
		Message:       message,
		Blocking:      blocking,
		RunID:         firstNonEmpty(call.Metadata["runId"], call.Metadata["sessionId"], call.Metadata["teamId"]),
		TeamID:        call.Metadata["teamId"],
		TaskID:        call.Metadata["taskId"],
		AgentID:       call.Metadata["agentId"],
		Reference:     key(call.Type, call.Name),
		Attempt:       attempt,
		Timestamp:     time.Now().UTC(),
	}
	if len(evidenceMetadata) > 0 {
		event.Evidence = &storage.PolicyOutcomeEvidence{Metadata: evidenceMetadata}
	}
	return event
}

func capabilityPolicyAction(outcome string) string {
	switch outcome {
	case "retrying":
		return "retry"
	case "paused":
		return "pause"
	case "replaced":
		return "replace"
	case "allowed", "allow":
		return "allow"
	default:
		return "block"
	}
}

func capabilityPolicyOperation(outcome string) string {
	if outcome == "retrying" {
		return "retry"
	}
	return "invoke"
}

func cloneMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
