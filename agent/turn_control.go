package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/tool"
)

type TurnBoundary string

const (
	TurnBoundaryBeforeModel TurnBoundary = "before_model"
	TurnBoundaryBeforeTools TurnBoundary = "before_tools"
	TurnBoundaryAfterTools  TurnBoundary = "after_tools"
	TurnBoundaryAfterAnswer TurnBoundary = "after_answer"
)

type ControlKind string

const (
	ControlSteer          ControlKind = "steer"
	ControlFollowUp       ControlKind = "follow_up"
	ControlAbort          ControlKind = "abort"
	ControlAbortAndPrompt ControlKind = "abort_and_prompt"
)

var ErrTurnControlAbort = errors.New("agent turn aborted by control channel")

// StreamRuleInterruptError stops one open provider stream after a host rule
// matches generated text, thinking, or tool arguments. The host queues the
// corresponding ControlMessage before returning this error from a stream hook;
// the loop then continues and drains that control at the next model boundary.
// KeepPartial controls whether already generated assistant content remains in
// model history. Usage is always retained.
type StreamRuleInterruptError struct {
	Reason      string
	KeepPartial bool
}

func (err *StreamRuleInterruptError) Error() string {
	if err == nil || err.Reason == "" {
		return "provider stream interrupted by host rule"
	}
	return "provider stream interrupted by host rule: " + err.Reason
}

const (
	controlIDMetadataKey   = "venat.control.id"
	controlKindMetadataKey = "venat.control.kind"
)

// ControlMessage is one durable host-owned instruction. Hosts should persist
// it before Enqueue or before returning it from TurnControl.Drain.
type ControlMessage struct {
	ID      string          `json:"id,omitempty"`
	Kind    ControlKind     `json:"kind"`
	Message message.Message `json:"message"`
}

// TurnControl supplies exactly-once control messages at deterministic loop
// boundaries. Drain reserves messages; Acknowledge removes only controls whose
// effects reached a durable turn boundary; Release makes uncommitted controls
// available to a retry.
type TurnControl interface {
	Drain(context.Context, TurnBoundary) ([]ControlMessage, error)
	Acknowledge(context.Context, []string) error
	Release(context.Context, []string) error
	Interrupts() <-chan struct{}
}

// ControlQueue is an in-process implementation suitable for one agent run.
// Durable hosts may implement TurnControl over their own outbox instead.
type ControlQueue struct {
	mu        sync.Mutex
	pending   []ControlMessage
	reserved  map[string]struct{}
	nextID    uint64
	interrupt chan struct{}
}

func NewControlQueue() *ControlQueue {
	return &ControlQueue{
		reserved:  make(map[string]struct{}),
		interrupt: make(chan struct{}, 1),
	}
}

func (queue *ControlQueue) Enqueue(control ControlMessage) error {
	if queue == nil {
		return fmt.Errorf("turn control queue is nil")
	}
	if err := validateControlMessage(control); err != nil {
		return err
	}
	control.Message = message.Clone(control.Message)
	queue.mu.Lock()
	if control.ID == "" {
		queue.nextID++
		control.ID = fmt.Sprintf("control-%d", queue.nextID)
	}
	for _, pending := range queue.pending {
		if pending.ID == control.ID {
			queue.mu.Unlock()
			return fmt.Errorf("duplicate turn control id %q", control.ID)
		}
	}
	queue.pending = append(queue.pending, control)
	queue.mu.Unlock()
	if control.Kind != ControlFollowUp {
		signalControlInterrupt(queue.interrupt)
	}
	return nil
}

func (queue *ControlQueue) Drain(_ context.Context, boundary TurnBoundary) ([]ControlMessage, error) {
	if queue == nil {
		return nil, nil
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	selected := make([]ControlMessage, 0)
	interrupting := false
	for _, control := range queue.pending {
		if _, reserved := queue.reserved[control.ID]; reserved || !deliverControlAt(control.Kind, boundary) {
			continue
		}
		queue.reserved[control.ID] = struct{}{}
		control.Message = message.Clone(control.Message)
		selected = append(selected, control)
		interrupting = interrupting || control.Kind != ControlFollowUp
	}
	if interrupting {
		select {
		case <-queue.interrupt:
		default:
		}
	}
	return selected, nil
}

func (queue *ControlQueue) Acknowledge(_ context.Context, ids []string) error {
	if queue == nil || len(ids) == 0 {
		return nil
	}
	targets := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		targets[id] = struct{}{}
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	remaining := queue.pending[:0]
	for _, control := range queue.pending {
		if _, acknowledged := targets[control.ID]; acknowledged {
			delete(queue.reserved, control.ID)
			continue
		}
		remaining = append(remaining, control)
	}
	for index := len(remaining); index < len(queue.pending); index++ {
		queue.pending[index] = ControlMessage{}
	}
	queue.pending = remaining
	return nil
}

func (queue *ControlQueue) Release(_ context.Context, ids []string) error {
	if queue == nil || len(ids) == 0 {
		return nil
	}
	targets := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		targets[id] = struct{}{}
	}
	queue.mu.Lock()
	interrupting := false
	for _, control := range queue.pending {
		if _, released := targets[control.ID]; released {
			delete(queue.reserved, control.ID)
			interrupting = interrupting || control.Kind != ControlFollowUp
		}
	}
	queue.mu.Unlock()
	if interrupting {
		signalControlInterrupt(queue.interrupt)
	}
	return nil
}

func signalControlInterrupt(interrupt chan struct{}) {
	select {
	case interrupt <- struct{}{}:
	default:
	}
}

func (queue *ControlQueue) Interrupts() <-chan struct{} {
	if queue == nil {
		return nil
	}
	return queue.interrupt
}

type turnControlSession struct {
	source    TurnControl
	mu        sync.Mutex
	delivered []string
	pending   map[string]struct{}
	kinds     map[string]ControlKind
}

func beginTurnControlSession(
	ctx context.Context,
	source TurnControl,
	applied []string,
) (*turnControlSession, error) {
	if source == nil {
		return nil, nil
	}
	if len(applied) > 0 {
		if err := source.Acknowledge(ctx, applied); err != nil {
			return nil, fmt.Errorf("acknowledge checkpointed turn controls: %w", err)
		}
	}
	return &turnControlSession{
		source:  source,
		pending: make(map[string]struct{}),
		kinds:   make(map[string]ControlKind),
	}, nil
}

func attachTurnControlSession(ctx context.Context, input *LoopInput) (*turnControlSession, error) {
	session, err := beginTurnControlSession(ctx, input.Control, input.AppliedControlIDs)
	if err != nil {
		return nil, err
	}
	if session != nil {
		input.Control = session
	}
	return session, nil
}

func releaseTurnControlSession(ctx context.Context, session *turnControlSession) error {
	if session == nil {
		return nil
	}
	return session.releasePending(ctx)
}

func (session *turnControlSession) Drain(ctx context.Context, boundary TurnBoundary) ([]ControlMessage, error) {
	batch, err := session.source.Drain(ctx, boundary)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(batch))
	seen := make(map[string]struct{}, len(batch))
	for index := range batch {
		if batch[index].ID == "" {
			_ = session.source.Release(context.WithoutCancel(ctx), ids)
			return nil, errors.New("turn control source returned a message without an id")
		}
		if _, duplicate := seen[batch[index].ID]; duplicate {
			ids = append(ids, batch[index].ID)
			_ = session.source.Release(context.WithoutCancel(ctx), ids)
			return nil, fmt.Errorf("turn control source returned duplicate id %q", batch[index].ID)
		}
		seen[batch[index].ID] = struct{}{}
		ids = append(ids, batch[index].ID)
	}
	session.mu.Lock()
	for _, control := range batch {
		if _, alreadyDelivered := session.pending[control.ID]; alreadyDelivered {
			continue
		}
		session.pending[control.ID] = struct{}{}
		session.kinds[control.ID] = control.Kind
		session.delivered = append(session.delivered, control.ID)
	}
	session.mu.Unlock()
	return batch, nil
}

func (session *turnControlSession) Acknowledge(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := session.source.Acknowledge(ctx, ids); err != nil {
		return err
	}
	session.mu.Lock()
	for _, id := range ids {
		delete(session.pending, id)
		delete(session.kinds, id)
	}
	session.compactDeliveredLocked()
	session.mu.Unlock()
	return nil
}

func (session *turnControlSession) Release(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := session.source.Release(ctx, ids); err != nil {
		return err
	}
	session.mu.Lock()
	for _, id := range ids {
		delete(session.pending, id)
		delete(session.kinds, id)
	}
	session.compactDeliveredLocked()
	session.mu.Unlock()
	return nil
}

func (session *turnControlSession) Interrupts() <-chan struct{} {
	return session.source.Interrupts()
}

func (session *turnControlSession) acknowledgePending(ctx context.Context) error {
	ids := session.pendingIDs()
	if err := session.Acknowledge(ctx, ids); err != nil {
		return fmt.Errorf("acknowledge turn controls: %w", err)
	}
	return nil
}

func (session *turnControlSession) releasePending(ctx context.Context) error {
	ids := session.pendingIDs()
	if err := session.Release(ctx, ids); err != nil {
		return fmt.Errorf("release turn controls: %w", err)
	}
	return nil
}

func (session *turnControlSession) pendingIDs() []string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return append([]string(nil), session.delivered...)
}

func sessionPendingIDs(session *turnControlSession) []string {
	if session == nil {
		return nil
	}
	return session.pendingIDs()
}

func (session *turnControlSession) hasAbort() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	for id := range session.pending {
		if session.kinds[id] == ControlAbort {
			return true
		}
	}
	return false
}

func (session *turnControlSession) compactDeliveredLocked() {
	kept := session.delivered[:0]
	for _, id := range session.delivered {
		if _, pending := session.pending[id]; pending {
			kept = append(kept, id)
		}
	}
	session.delivered = kept
}

func validateControlMessage(control ControlMessage) error {
	switch control.Kind {
	case ControlSteer, ControlFollowUp, ControlAbort, ControlAbortAndPrompt:
	default:
		return fmt.Errorf("unknown turn control kind %q", control.Kind)
	}
	if control.Kind != ControlAbort && control.Message.Role == "" {
		return fmt.Errorf("turn control %s requires a message", control.Kind)
	}
	return nil
}

func deliverControlAt(kind ControlKind, boundary TurnBoundary) bool {
	if boundary == TurnBoundaryAfterAnswer {
		return true
	}
	return kind != ControlFollowUp
}

func drainTurnControl(ctx context.Context, control TurnControl, boundary TurnBoundary) ([]ControlMessage, error) {
	if control == nil {
		return nil, nil
	}
	batch, err := control.Drain(ctx, boundary)
	if err != nil {
		return nil, err
	}
	for index := range batch {
		if err := validateControlMessage(batch[index]); err != nil {
			return nil, err
		}
		batch[index].Message = message.Clone(batch[index].Message)
	}
	return batch, nil
}

func controlMessages(batch []ControlMessage) []message.Message {
	messages := make([]message.Message, 0, len(batch))
	for _, control := range batch {
		current := message.Clone(control.Message)
		if control.Kind == ControlAbort {
			current = message.Message{
				Role:       message.RoleSystem,
				Kind:       message.KindCustom,
				Visibility: message.VisibilityPrivate,
			}
		}
		if current.Metadata == nil {
			current.Metadata = make(map[string]string, 2)
		}
		current.Metadata[controlIDMetadataKey] = control.ID
		current.Metadata[controlKindMetadataKey] = string(control.Kind)
		messages = append(messages, current)
	}
	return messages
}

func controlAborts(batch []ControlMessage) bool {
	for _, control := range batch {
		if control.Kind == ControlAbort {
			return true
		}
	}
	return false
}

func cancelledToolResults(calls []tool.Call) []tool.Result {
	results := make([]tool.Result, len(calls))
	for index, call := range calls {
		results[index] = tool.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    "tool call cancelled before execution by turn control",
			IsError:    true,
		}
		results[index].SyncLegacyContent()
	}
	return results
}

func controlledToolContext(parent context.Context, control TurnControl) (context.Context, func()) {
	if control == nil {
		return parent, func() {}
	}
	interrupts := control.Interrupts()
	if interrupts == nil {
		return parent, func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		select {
		case <-interrupts:
			cancel()
		case <-done:
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		close(done)
		cancel()
	}
}

func completeCancelledToolResults(
	calls []tool.Call,
	completed []tool.Result,
	notExecuted map[string]struct{},
) []tool.Result {
	byID := make(map[string]tool.Result, len(completed))
	for _, result := range completed {
		byID[result.ToolCallID] = result
	}
	results := make([]tool.Result, 0, len(completed)+len(notExecuted))
	for _, call := range calls {
		if result, ok := byID[call.ID]; ok {
			results = append(results, result)
			continue
		}
		if _, proven := notExecuted[call.ID]; proven {
			results = append(results, cancelledToolResults([]tool.Call{call})[0])
		}
	}
	return results
}
