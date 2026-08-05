package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

func StoreProviderToCore(inner api.StoreProvider) core.StoreProvider {
	if inner == nil {
		return nil
	}
	base := apiStoreProviderAdapter{inner: inner}
	if subscriber, ok := inner.(api.BlackboardSubscriber); ok {
		return apiStoreProviderSubscriberAdapter{apiStoreProviderAdapter: base, subscriber: subscriber}
	}
	return base
}

type apiStoreProviderAdapter struct{ inner api.StoreProvider }

func (a apiStoreProviderAdapter) Begin(ctx context.Context) (ports.UnitOfWork, error) {
	uow, err := a.inner.Begin(ctx)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UnitOfWorkToCore(uow), nil
}

func (a apiStoreProviderAdapter) Capabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	reporter, ok := a.inner.(api.CapabilityReporter)
	if !ok {
		return ports.DefaultStoreCapabilities(), nil
	}
	capabilities, err := reporter.Capabilities(ctx)
	if err != nil {
		return ports.StoreCapabilities{}, ErrorToCore(err)
	}
	return ports.StoreCapabilities{
		SupportsTransactions:          capabilities.SupportsTransactions,
		SupportsBlackboardSubscribe:   capabilities.SupportsBlackboardSubscribe,
		SupportsListPending:           capabilities.SupportsListPending,
		SupportsConcurrentWriters:     capabilities.SupportsConcurrentWriters,
		SupportsDeadLetterRequeue:     capabilities.SupportsDeadLetterRequeue,
		SupportsDefinitionSnapshots:   capabilities.SupportsDefinitionSnapshots,
		SupportsAdmissionReservations: capabilities.SupportsAdmissionReservations,
		SupportsResourceClaims:        capabilities.SupportsResourceClaims,
	}, nil
}

func (a apiStoreProviderAdapter) Close(ctx context.Context) error {
	closer, ok := a.inner.(api.ProviderCloser)
	if !ok {
		return nil
	}
	return ErrorToCore(closer.Close(ctx))
}

type apiStoreProviderSubscriberAdapter struct {
	apiStoreProviderAdapter
	subscriber api.BlackboardSubscriber
}

func (a apiStoreProviderSubscriberAdapter) Subscribe(ctx context.Context, runID string, selector model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error) {
	items, cancel, err := a.subscriber.Subscribe(ctx, runID, BlackboardSelectorFromModel(selector))
	if err != nil {
		return nil, nil, ErrorToCore(err)
	}
	out := make(chan model.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- BlackboardItemToModel(item)
		}
	}()
	return out, func() error { return ErrorToCore(cancel()) }, nil
}

func UnitOfWorkToCore(inner api.UnitOfWork) ports.UnitOfWork {
	if inner == nil {
		return nil
	}
	base := apiUnitOfWorkAdapter{inner: inner}
	definitions, hasDefinitions := inner.(api.AgentDefinitionUnitOfWork)
	admission, hasAdmission := inner.(api.AdmissionReservationUnitOfWork)
	claims, hasClaims := inner.(api.ResourceClaimUnitOfWork)
	switch {
	case hasDefinitions && hasAdmission && hasClaims:
		return apiDefinitionGovernanceUnitOfWorkAdapter{
			apiUnitOfWorkAdapter: base, definitions: definitions, admission: admission, claims: claims,
		}
	case hasDefinitions && hasAdmission:
		return apiDefinitionAdmissionUnitOfWorkAdapter{
			apiUnitOfWorkAdapter: base, definitions: definitions, admission: admission,
		}
	case hasDefinitions && hasClaims:
		return apiDefinitionResourceClaimUnitOfWorkAdapter{
			apiUnitOfWorkAdapter: base, definitions: definitions, claims: claims,
		}
	case hasAdmission && hasClaims:
		return apiGovernanceUnitOfWorkAdapter{apiUnitOfWorkAdapter: base, admission: admission, claims: claims}
	case hasDefinitions:
		return apiDefinitionUnitOfWorkAdapter{apiUnitOfWorkAdapter: base, definitions: definitions}
	case hasAdmission:
		return apiAdmissionUnitOfWorkAdapter{apiUnitOfWorkAdapter: base, admission: admission}
	case hasClaims:
		return apiResourceClaimUnitOfWorkAdapter{apiUnitOfWorkAdapter: base, claims: claims}
	default:
		return base
	}
}

type apiUnitOfWorkAdapter struct{ inner api.UnitOfWork }

type apiDefinitionUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	definitions api.AgentDefinitionUnitOfWork
}

func (a apiDefinitionUnitOfWorkAdapter) AgentDefinitions() ports.AgentDefinitionStore {
	return apiAgentDefinitionStoreAdapter{inner: a.definitions.AgentDefinitions()}
}

type apiDefinitionAdmissionUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	definitions api.AgentDefinitionUnitOfWork
	admission   api.AdmissionReservationUnitOfWork
}

func (a apiDefinitionAdmissionUnitOfWorkAdapter) AgentDefinitions() ports.AgentDefinitionStore {
	return apiAgentDefinitionStoreAdapter{inner: a.definitions.AgentDefinitions()}
}

func (a apiDefinitionAdmissionUnitOfWorkAdapter) AdmissionReservations() ports.AdmissionReservationStore {
	return apiAdmissionReservationStoreAdapter{inner: a.admission.AdmissionReservations()}
}

type apiDefinitionResourceClaimUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	definitions api.AgentDefinitionUnitOfWork
	claims      api.ResourceClaimUnitOfWork
}

func (a apiDefinitionResourceClaimUnitOfWorkAdapter) AgentDefinitions() ports.AgentDefinitionStore {
	return apiAgentDefinitionStoreAdapter{inner: a.definitions.AgentDefinitions()}
}

func (a apiDefinitionResourceClaimUnitOfWorkAdapter) ResourceClaims() ports.ResourceClaimStore {
	return apiResourceClaimStoreAdapter{inner: a.claims.ResourceClaims()}
}

type apiDefinitionGovernanceUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	definitions api.AgentDefinitionUnitOfWork
	admission   api.AdmissionReservationUnitOfWork
	claims      api.ResourceClaimUnitOfWork
}

func (a apiDefinitionGovernanceUnitOfWorkAdapter) AgentDefinitions() ports.AgentDefinitionStore {
	return apiAgentDefinitionStoreAdapter{inner: a.definitions.AgentDefinitions()}
}

func (a apiDefinitionGovernanceUnitOfWorkAdapter) AdmissionReservations() ports.AdmissionReservationStore {
	return apiAdmissionReservationStoreAdapter{inner: a.admission.AdmissionReservations()}
}

func (a apiDefinitionGovernanceUnitOfWorkAdapter) ResourceClaims() ports.ResourceClaimStore {
	return apiResourceClaimStoreAdapter{inner: a.claims.ResourceClaims()}
}

type apiAdmissionUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	admission api.AdmissionReservationUnitOfWork
}

func (a apiAdmissionUnitOfWorkAdapter) AdmissionReservations() ports.AdmissionReservationStore {
	return apiAdmissionReservationStoreAdapter{inner: a.admission.AdmissionReservations()}
}

type apiResourceClaimUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	claims api.ResourceClaimUnitOfWork
}

func (a apiResourceClaimUnitOfWorkAdapter) ResourceClaims() ports.ResourceClaimStore {
	return apiResourceClaimStoreAdapter{inner: a.claims.ResourceClaims()}
}

type apiGovernanceUnitOfWorkAdapter struct {
	apiUnitOfWorkAdapter
	admission api.AdmissionReservationUnitOfWork
	claims    api.ResourceClaimUnitOfWork
}

func (a apiGovernanceUnitOfWorkAdapter) AdmissionReservations() ports.AdmissionReservationStore {
	return apiAdmissionReservationStoreAdapter{inner: a.admission.AdmissionReservations()}
}

func (a apiGovernanceUnitOfWorkAdapter) ResourceClaims() ports.ResourceClaimStore {
	return apiResourceClaimStoreAdapter{inner: a.claims.ResourceClaims()}
}

func (a apiUnitOfWorkAdapter) Runs() ports.RunStore { return apiRunStoreAdapter{inner: a.inner.Runs()} }
func (a apiUnitOfWorkAdapter) Tasks() ports.TaskStore {
	return apiTaskStoreAdapter{inner: a.inner.Tasks()}
}

func (a apiUnitOfWorkAdapter) Events() ports.EventStore {
	return apiEventStoreAdapter{inner: a.inner.Events()}
}

func (a apiUnitOfWorkAdapter) Blackboard() ports.BlackboardReadWriter {
	return apiBlackboardStoreAdapter{inner: a.inner.Blackboard()}
}

func (a apiUnitOfWorkAdapter) MailboxOutbox() ports.MailboxOutboxStore {
	return apiMailboxOutboxStoreAdapter{inner: a.inner.MailboxOutbox()}
}

func (a apiUnitOfWorkAdapter) UserMessages() ports.UserMessageStore {
	return apiUserMessageStoreAdapter{inner: a.inner.UserMessages()}
}

func (a apiUnitOfWorkAdapter) Trace() ports.TraceStore {
	return apiTraceStoreAdapter{inner: a.inner.Trace()}
}

func (a apiUnitOfWorkAdapter) Leases() ports.LeaseStore {
	return apiLeaseStoreAdapter{inner: a.inner.Leases()}
}

func (a apiUnitOfWorkAdapter) Approvals() ports.ApprovalStore {
	return apiApprovalStoreAdapter{inner: a.inner.Approvals()}
}

func (a apiUnitOfWorkAdapter) ResumeTokens() ports.ResumeTokenStore {
	return apiResumeTokenStoreAdapter{inner: a.inner.ResumeTokens()}
}

func (a apiUnitOfWorkAdapter) ActionAttempts() ports.ActionAttemptStore {
	return apiActionAttemptStoreAdapter{inner: a.inner.ActionAttempts()}
}

func (a apiUnitOfWorkAdapter) AgentProfiles() ports.AgentProfileStore {
	return apiAgentProfileStoreAdapter{inner: a.inner.AgentProfiles()}
}

func (a apiUnitOfWorkAdapter) CapabilityCatalog() ports.CapabilityStore {
	return apiCapabilityStoreAdapter{inner: a.inner.CapabilityCatalog()}
}

func (a apiUnitOfWorkAdapter) UsageRecords() ports.UsageStore {
	return apiUsageStoreAdapter{inner: a.inner.UsageRecords()}
}

func (a apiUnitOfWorkAdapter) DeadLetters() ports.DeadLetterStore {
	return apiDeadLetterStoreAdapter{inner: a.inner.DeadLetters()}
}

func (a apiUnitOfWorkAdapter) Handoffs() ports.HandoffStore {
	return apiHandoffStoreAdapter{inner: a.inner.Handoffs()}
}

func (a apiUnitOfWorkAdapter) TeamStates() ports.TeamStateStore {
	return apiTeamStateStoreAdapter{inner: a.inner.TeamStates()}
}

func (a apiUnitOfWorkAdapter) AgentInstances() ports.AgentInstanceStore {
	return apiAgentInstanceStoreAdapter{inner: a.inner.AgentInstances()}
}

func (a apiUnitOfWorkAdapter) Commit(ctx context.Context) error {
	return ErrorToCore(a.inner.Commit(ctx))
}

func (a apiUnitOfWorkAdapter) Rollback(ctx context.Context) error {
	return ErrorToCore(a.inner.Rollback(ctx))
}

type apiRunStoreAdapter struct{ inner api.RunStore }

func (a apiRunStoreAdapter) SaveRun(ctx context.Context, run model.Run) error {
	return ErrorToCore(a.inner.SaveRun(ctx, RunFromModel(run)))
}

func (a apiRunStoreAdapter) LoadRun(ctx context.Context, runID string) (model.Run, error) {
	run, err := a.inner.LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, ErrorToCore(err)
	}
	return RunToModel(run), nil
}

func (a apiRunStoreAdapter) ListRuns(ctx context.Context, sel model.RunSelector) ([]model.Run, error) {
	runs, err := a.inner.ListRuns(ctx, RunSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return RunsToModel(runs), nil
}

type apiTaskStoreAdapter struct{ inner api.TaskStore }

func (a apiTaskStoreAdapter) SaveTask(ctx context.Context, task model.Task) error {
	return ErrorToCore(a.inner.SaveTask(ctx, TaskFromModel(task)))
}

func (a apiTaskStoreAdapter) LoadTask(ctx context.Context, runID, taskID string) (model.Task, error) {
	task, err := a.inner.LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Task{}, ErrorToCore(err)
	}
	return TaskToModel(task), nil
}

func (a apiTaskStoreAdapter) ListTasks(ctx context.Context, runID string) ([]model.Task, error) {
	tasks, err := a.inner.ListTasks(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TasksToModel(tasks), nil
}

type apiEventStoreAdapter struct{ inner api.EventStore }

func (a apiEventStoreAdapter) AppendEvent(ctx context.Context, event model.Event) error {
	return ErrorToCore(a.inner.AppendEvent(ctx, EventFromModel(event)))
}

func (a apiEventStoreAdapter) ListEvents(ctx context.Context, runID string) ([]model.Event, error) {
	events, err := a.inner.ListEvents(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return EventsToModel(events), nil
}

func (a apiEventStoreAdapter) ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]model.Event, error) {
	events, err := a.inner.ListAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return EventsToModel(events), nil
}

type apiTraceStoreAdapter struct{ inner api.TraceStore }

func (a apiTraceStoreAdapter) SaveTraceSpan(ctx context.Context, span model.TraceSpan) error {
	return ErrorToCore(a.inner.SaveTraceSpan(ctx, TraceSpanFromModel(span)))
}

func (a apiTraceStoreAdapter) ListTraceSpans(ctx context.Context, runID string) ([]model.TraceSpan, error) {
	spans, err := a.inner.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TraceSpansToModel(spans), nil
}

func (a apiTraceStoreAdapter) LoadTraceSpan(ctx context.Context, spanID string) (model.TraceSpan, error) {
	updater, ok := a.inner.(api.TraceSpanUpdater)
	if !ok {
		return model.TraceSpan{}, fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration)
	}
	span, err := updater.LoadTraceSpan(ctx, spanID)
	if err != nil {
		return model.TraceSpan{}, ErrorToCore(err)
	}
	return TraceSpanToModel(span), nil
}

func (a apiTraceStoreAdapter) UpdateTraceSpan(ctx context.Context, span model.TraceSpan) error {
	updater, ok := a.inner.(api.TraceSpanUpdater)
	if !ok {
		return fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration)
	}
	return ErrorToCore(updater.UpdateTraceSpan(ctx, TraceSpanFromModel(span)))
}

type apiBlackboardStoreAdapter struct{ inner api.BlackboardReadWriter }

func (a apiBlackboardStoreAdapter) WriteItem(ctx context.Context, item model.BlackboardItem) error {
	return ErrorToCore(a.inner.WriteItem(ctx, BlackboardItemFromModel(item)))
}

func (a apiBlackboardStoreAdapter) SelectItems(ctx context.Context, runID string, selector model.BlackboardSelector) ([]model.BlackboardItem, error) {
	items, err := a.inner.SelectItems(ctx, runID, BlackboardSelectorFromModel(selector))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return BlackboardItemsToModel(items), nil
}

type apiMailboxOutboxStoreAdapter struct{ inner api.MailboxOutboxStore }

func (a apiMailboxOutboxStoreAdapter) QueueEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return ErrorToCore(a.inner.QueueEnvelope(ctx, TaskEnvelopeFromModel(env)))
}

func (a apiMailboxOutboxStoreAdapter) LoadEnvelope(ctx context.Context, envelopeID string) (model.TaskEnvelope, error) {
	env, err := a.inner.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return model.TaskEnvelope{}, ErrorToCore(err)
	}
	return TaskEnvelopeToModel(env), nil
}

func (a apiMailboxOutboxStoreAdapter) UpdateEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return ErrorToCore(a.inner.UpdateEnvelope(ctx, TaskEnvelopeFromModel(env)))
}

func (a apiMailboxOutboxStoreAdapter) ListEnvelopes(ctx context.Context, runID string) ([]model.TaskEnvelope, error) {
	envelopes, err := a.inner.ListEnvelopes(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TaskEnvelopesToModel(envelopes), nil
}

type apiUserMessageStoreAdapter struct{ inner api.UserMessageStore }

func (a apiUserMessageStoreAdapter) QueueMessage(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.QueueMessage(ctx, UserMessageFromModel(message)))
}

func (a apiUserMessageStoreAdapter) LoadMessage(ctx context.Context, runID, messageID string) (model.UserMessage, error) {
	message, err := a.inner.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return model.UserMessage{}, ErrorToCore(err)
	}
	return UserMessageToModel(message), nil
}

func (a apiUserMessageStoreAdapter) UpdateMessage(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.UpdateMessage(ctx, UserMessageFromModel(message)))
}

func (a apiUserMessageStoreAdapter) ListMessages(ctx context.Context, runID string) ([]model.UserMessage, error) {
	messages, err := a.inner.ListMessages(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

func (a apiUserMessageStoreAdapter) ListPendingFor(ctx context.Context, sel model.UserMessageSelector) ([]model.UserMessage, error) {
	messages, err := a.inner.ListPendingFor(ctx, UserMessageSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

func (a apiUserMessageStoreAdapter) ListQueuedMessages(ctx context.Context) ([]model.UserMessage, error) {
	scanner, ok := a.inner.(api.UserMessageOutboxScanner)
	if !ok {
		return nil, fmt.Errorf("user message store does not support queued outbox scanning: %w", model.ErrInvalidConfiguration)
	}
	messages, err := scanner.ListQueuedMessages(ctx)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

type apiLeaseStoreAdapter struct{ inner api.LeaseStore }

func (a apiLeaseStoreAdapter) SaveLease(ctx context.Context, lease model.TaskExecutionLease) error {
	return ErrorToCore(a.inner.SaveLease(ctx, TaskExecutionLeaseFromModel(lease)))
}

func (a apiLeaseStoreAdapter) LoadLease(ctx context.Context, leaseID string) (model.TaskExecutionLease, error) {
	lease, err := a.inner.LoadLease(ctx, leaseID)
	if err != nil {
		return model.TaskExecutionLease{}, ErrorToCore(err)
	}
	return TaskExecutionLeaseToModel(lease), nil
}

func (a apiLeaseStoreAdapter) ActiveLeaseForTask(ctx context.Context, runID, taskID string) (model.TaskExecutionLease, bool, error) {
	lease, ok, err := a.inner.ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil {
		return model.TaskExecutionLease{}, false, ErrorToCore(err)
	}
	return TaskExecutionLeaseToModel(lease), ok, nil
}

func (a apiLeaseStoreAdapter) AcquireWithExpectedVersion(ctx context.Context, lease model.TaskExecutionLease, expectedVersion uint64) (bool, error) {
	ok, err := a.inner.AcquireWithExpectedVersion(ctx, TaskExecutionLeaseFromModel(lease), expectedVersion)
	if err != nil {
		return false, ErrorToCore(err)
	}
	return ok, nil
}

func (a apiLeaseStoreAdapter) ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error) {
	ok, err := a.inner.ExtendLease(ctx, leaseID, workerID, newExpiry)
	if err != nil {
		return false, ErrorToCore(err)
	}
	return ok, nil
}

func (a apiLeaseStoreAdapter) ReleaseExpiredLease(ctx context.Context, leaseID string, expectedVersion uint64, releasedAt time.Time) (bool, error) {
	ok, err := a.inner.ReleaseExpiredLease(ctx, leaseID, expectedVersion, releasedAt)
	if err != nil {
		return false, ErrorToCore(err)
	}
	return ok, nil
}

type apiApprovalStoreAdapter struct{ inner api.ApprovalStore }

func (a apiApprovalStoreAdapter) SaveApproval(ctx context.Context, approval model.ApprovalRequest) error {
	return ErrorToCore(a.inner.SaveApproval(ctx, ApprovalRequestFromModel(approval)))
}

func (a apiApprovalStoreAdapter) LoadApproval(ctx context.Context, approvalID string) (model.ApprovalRequest, error) {
	approval, err := a.inner.LoadApproval(ctx, approvalID)
	if err != nil {
		return model.ApprovalRequest{}, ErrorToCore(err)
	}
	return ApprovalRequestToModel(approval), nil
}

type apiResumeTokenStoreAdapter struct{ inner api.ResumeTokenStore }

func (a apiResumeTokenStoreAdapter) SaveResumeToken(ctx context.Context, token model.ResumeToken) error {
	return ErrorToCore(a.inner.SaveResumeToken(ctx, ResumeTokenFromModel(token)))
}

func (a apiResumeTokenStoreAdapter) LoadResumeToken(ctx context.Context, tokenID string) (model.ResumeToken, error) {
	token, err := a.inner.LoadResumeToken(ctx, tokenID)
	if err != nil {
		return model.ResumeToken{}, ErrorToCore(err)
	}
	return ResumeTokenToModel(token), nil
}

func (a apiResumeTokenStoreAdapter) ListPending(ctx context.Context, sel model.ResumeTokenSelector) ([]model.ResumeToken, error) {
	tokens, err := a.inner.ListPending(ctx, ResumeTokenSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return ResumeTokensToModel(tokens), nil
}

type apiActionAttemptStoreAdapter struct{ inner api.ActionAttemptStore }

func (a apiActionAttemptStoreAdapter) SaveActionAttempt(ctx context.Context, attempt model.ActionAttempt) error {
	return ErrorToCore(a.inner.SaveActionAttempt(ctx, ActionAttemptFromModel(attempt)))
}

func (a apiActionAttemptStoreAdapter) LoadActionAttempt(ctx context.Context, attemptID string) (model.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttempt(ctx, attemptID)
	if err != nil {
		return model.ActionAttempt{}, ErrorToCore(err)
	}
	return ActionAttemptToModel(attempt), nil
}

func (a apiActionAttemptStoreAdapter) LoadActionAttemptByIdempotencyKey(ctx context.Context, runID string, taskID string, toolName string, key string) (model.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttemptByIdempotencyKey(ctx, runID, taskID, toolName, key)
	if err != nil {
		return model.ActionAttempt{}, ErrorToCore(err)
	}
	return ActionAttemptToModel(attempt), nil
}

func (a apiActionAttemptStoreAdapter) ListActionAttempts(ctx context.Context, sel model.ActionAttemptSelector) ([]model.ActionAttempt, error) {
	attempts, err := a.inner.ListActionAttempts(ctx, ActionAttemptSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return ActionAttemptsToModel(attempts), nil
}

func (a apiActionAttemptStoreAdapter) ResolveActionAttempt(ctx context.Context, attempt model.ActionAttempt) (bool, error) {
	resolved, err := a.inner.ResolveActionAttempt(ctx, ActionAttemptFromModel(attempt))
	return resolved, ErrorToCore(err)
}

type apiAgentProfileStoreAdapter struct{ inner api.AgentProfileStore }

func (a apiAgentProfileStoreAdapter) SaveAgentProfile(ctx context.Context, profile model.AgentProfile) error {
	return ErrorToCore(a.inner.SaveAgentProfile(ctx, AgentProfileFromModel(profile)))
}

func (a apiAgentProfileStoreAdapter) LoadAgentProfile(ctx context.Context, id string) (model.AgentProfile, error) {
	profile, err := a.inner.LoadAgentProfile(ctx, id)
	if err != nil {
		return model.AgentProfile{}, ErrorToCore(err)
	}
	return AgentProfileToModel(profile), nil
}

func (a apiAgentProfileStoreAdapter) ListAgentProfiles(ctx context.Context, sel model.AgentSelector) ([]model.AgentProfile, error) {
	profiles, err := a.inner.ListAgentProfiles(ctx, AgentSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return AgentProfilesToModel(profiles), nil
}

type apiCapabilityStoreAdapter struct{ inner api.CapabilityStore }

func (a apiCapabilityStoreAdapter) SaveCapability(ctx context.Context, capability model.Capability) error {
	return ErrorToCore(a.inner.SaveCapability(ctx, CapabilityFromModel(capability)))
}

func (a apiCapabilityStoreAdapter) LoadCapability(ctx context.Context, name string, agentID string) (model.Capability, error) {
	capability, err := a.inner.LoadCapability(ctx, name, agentID)
	if err != nil {
		return model.Capability{}, ErrorToCore(err)
	}
	return CapabilityToModel(capability), nil
}

func (a apiCapabilityStoreAdapter) ListCapabilities(ctx context.Context, sel model.CapabilitySelector) ([]model.Capability, error) {
	caps, err := a.inner.ListCapabilities(ctx, CapabilitySelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return CapabilitiesToModel(caps), nil
}

type apiUsageStoreAdapter struct{ inner api.UsageStore }

func (a apiUsageStoreAdapter) AppendUsage(ctx context.Context, rec model.UsageRecord) error {
	return ErrorToCore(a.inner.AppendUsage(ctx, UsageRecordFromModel(rec)))
}

func (a apiUsageStoreAdapter) QueryUsage(ctx context.Context, sel model.UsageSelector) ([]model.UsageRecord, error) {
	records, err := a.inner.QueryUsage(ctx, UsageSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UsageRecordsToModel(records), nil
}

func (a apiUsageStoreAdapter) SumCredits(ctx context.Context, sel model.UsageSelector) (int64, error) {
	sum, err := a.inner.SumCredits(ctx, UsageSelectorFromModel(sel))
	if err != nil {
		return 0, ErrorToCore(err)
	}
	return sum, nil
}

type apiDeadLetterStoreAdapter struct{ inner api.DeadLetterStore }

func (a apiDeadLetterStoreAdapter) AppendDeadLetter(ctx context.Context, entry model.DeadLetterEntry) error {
	return ErrorToCore(a.inner.AppendDeadLetter(ctx, DeadLetterEntryFromModel(entry)))
}

func (a apiDeadLetterStoreAdapter) ListDeadLetters(ctx context.Context, sel model.DeadLetterSelector) ([]model.DeadLetterEntry, error) {
	entries, err := a.inner.ListDeadLetters(ctx, DeadLetterSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return DeadLetterEntriesToModel(entries), nil
}

func (a apiDeadLetterStoreAdapter) Requeue(ctx context.Context, deadLetterID string) error {
	return ErrorToCore(a.inner.Requeue(ctx, deadLetterID))
}

type apiHandoffStoreAdapter struct{ inner api.HandoffStore }

func (a apiHandoffStoreAdapter) SaveHandoff(ctx context.Context, record model.HandoffRecord) error {
	return ErrorToCore(a.inner.SaveHandoff(ctx, HandoffRecordFromModel(record)))
}

func (a apiHandoffStoreAdapter) LoadHandoff(ctx context.Context, runID, handoffID string) (model.HandoffRecord, error) {
	record, err := a.inner.LoadHandoff(ctx, runID, handoffID)
	if err != nil {
		return model.HandoffRecord{}, ErrorToCore(err)
	}
	return HandoffRecordToModel(record), nil
}

func (a apiHandoffStoreAdapter) ListHandoffs(ctx context.Context, sel model.HandoffSelector) ([]model.HandoffRecord, error) {
	records, err := a.inner.ListHandoffs(ctx, HandoffSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return HandoffRecordsToModel(records), nil
}

type apiTeamStateStoreAdapter struct{ inner api.TeamStateStore }

func (a apiTeamStateStoreAdapter) SaveTeamState(ctx context.Context, record model.TeamStateRecord) error {
	return ErrorToCore(a.inner.SaveTeamState(ctx, TeamStateRecordFromModel(record)))
}

func (a apiTeamStateStoreAdapter) LoadTeamState(ctx context.Context, runID string) (model.TeamStateRecord, error) {
	record, err := a.inner.LoadTeamState(ctx, runID)
	if err != nil {
		return model.TeamStateRecord{}, ErrorToCore(err)
	}
	return TeamStateRecordToModel(record), nil
}

type apiAgentInstanceStoreAdapter struct{ inner api.AgentInstanceStore }

func (a apiAgentInstanceStoreAdapter) SaveAgentInstance(ctx context.Context, record model.AgentInstanceRecord) error {
	return ErrorToCore(a.inner.SaveAgentInstance(ctx, AgentInstanceRecordFromModel(record)))
}

func (a apiAgentInstanceStoreAdapter) LoadAgentInstance(ctx context.Context, id string) (model.AgentInstanceRecord, error) {
	record, err := a.inner.LoadAgentInstance(ctx, id)
	if err != nil {
		return model.AgentInstanceRecord{}, ErrorToCore(err)
	}
	return AgentInstanceRecordToModel(record), nil
}

func (a apiAgentInstanceStoreAdapter) ListAgentInstances(ctx context.Context, sel model.AgentInstanceSelector) ([]model.AgentInstanceRecord, error) {
	records, err := a.inner.ListAgentInstances(ctx, AgentInstanceSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return AgentInstanceRecordsToModel(records), nil
}

type apiAgentDefinitionStoreAdapter struct{ inner api.AgentDefinitionStore }

func (a apiAgentDefinitionStoreAdapter) SaveAgentDefinitionSnapshot(ctx context.Context, snapshot model.AgentDefinitionSnapshot) error {
	converted, err := AgentDefinitionSnapshotFromModel(snapshot)
	if err != nil {
		return ErrorToCore(err)
	}
	return ErrorToCore(a.inner.SaveAgentDefinitionSnapshot(ctx, converted))
}

func (a apiAgentDefinitionStoreAdapter) LoadAgentDefinitionSnapshot(ctx context.Context, definitionID, version string) (model.AgentDefinitionSnapshot, error) {
	snapshot, err := a.inner.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
	if err != nil {
		return model.AgentDefinitionSnapshot{}, ErrorToCore(err)
	}
	converted, err := AgentDefinitionSnapshotToModel(snapshot)
	if err != nil {
		return model.AgentDefinitionSnapshot{}, ErrorToCore(err)
	}
	return converted, nil
}

func (a apiAgentDefinitionStoreAdapter) ListAgentDefinitionSnapshots(ctx context.Context, selector model.AgentDefinitionSnapshotSelector) ([]model.AgentDefinitionSnapshot, error) {
	snapshots, err := a.inner.ListAgentDefinitionSnapshots(ctx, AgentDefinitionSnapshotSelectorFromModel(selector))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	out := make([]model.AgentDefinitionSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		converted, convertErr := AgentDefinitionSnapshotToModel(snapshot)
		if convertErr != nil {
			return nil, ErrorToCore(convertErr)
		}
		out = append(out, converted)
	}
	return out, nil
}

type apiAdmissionReservationStoreAdapter struct {
	inner api.AdmissionReservationStore
}

func (a apiAdmissionReservationStoreAdapter) PreviewAdmission(ctx context.Context, request model.AdmissionRequest) (model.AdmissionDecision, error) {
	decision, err := a.inner.PreviewAdmission(ctx, AdmissionRequestFromModel(request))
	return AdmissionDecisionToModel(decision), ErrorToCore(err)
}

func (a apiAdmissionReservationStoreAdapter) ReserveAdmission(ctx context.Context, request model.AdmissionRequest) (model.AdmissionDecision, error) {
	decision, err := a.inner.ReserveAdmission(ctx, AdmissionRequestFromModel(request))
	return AdmissionDecisionToModel(decision), ErrorToCore(err)
}

func (a apiAdmissionReservationStoreAdapter) TransitionAdmission(ctx context.Context, transition model.AdmissionTransition) (model.AdmissionDecision, error) {
	decision, err := a.inner.TransitionAdmission(ctx, AdmissionTransitionFromModel(transition))
	return AdmissionDecisionToModel(decision), ErrorToCore(err)
}

func (a apiAdmissionReservationStoreAdapter) LoadAdmissionReservation(ctx context.Context, id string) (model.AdmissionReservation, error) {
	reservation, err := a.inner.LoadAdmissionReservation(ctx, id)
	return AdmissionReservationToModel(reservation), ErrorToCore(err)
}

func (a apiAdmissionReservationStoreAdapter) ListAdmissionReservations(ctx context.Context, selector model.AdmissionReservationSelector) ([]model.AdmissionReservation, error) {
	reservations, err := a.inner.ListAdmissionReservations(ctx, AdmissionReservationSelectorFromModel(selector))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	out := make([]model.AdmissionReservation, 0, len(reservations))
	for _, reservation := range reservations {
		out = append(out, AdmissionReservationToModel(reservation))
	}
	return out, nil
}

type apiResourceClaimStoreAdapter struct {
	inner api.ResourceClaimStore
}

func (a apiResourceClaimStoreAdapter) AcquireResourceClaims(ctx context.Context, request model.ResourceClaimRequest) (model.ResourceClaimDecision, error) {
	decision, err := a.inner.AcquireResourceClaims(ctx, ResourceClaimRequestFromModel(request))
	return ResourceClaimDecisionToModel(decision), ErrorToCore(err)
}

func (a apiResourceClaimStoreAdapter) TransitionResourceClaims(ctx context.Context, request model.ResourceClaimTransitionRequest) (model.ResourceClaimDecision, error) {
	decision, err := a.inner.TransitionResourceClaims(ctx, ResourceClaimTransitionRequestFromModel(request))
	return ResourceClaimDecisionToModel(decision), ErrorToCore(err)
}

func (a apiResourceClaimStoreAdapter) LoadResourceClaim(ctx context.Context, id string) (model.ResourceClaim, error) {
	claim, err := a.inner.LoadResourceClaim(ctx, id)
	return ResourceClaimToModel(claim), ErrorToCore(err)
}

func (a apiResourceClaimStoreAdapter) ListResourceClaims(ctx context.Context, selector model.ResourceClaimSelector) ([]model.ResourceClaim, error) {
	claims, err := a.inner.ListResourceClaims(ctx, ResourceClaimSelectorFromModel(selector))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return ResourceClaimsToModel(claims), nil
}
