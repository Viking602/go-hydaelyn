package model

import "time"

type EventType string

const (
	EventRunStarted                  EventType = "RunStarted"
	EventRunStatusChanged            EventType = "RunStatusChanged"
	EventIntentAnalyzed              EventType = "IntentAnalyzed"
	EventPlanCreated                 EventType = "PlanCreated"
	EventPlanValidated               EventType = "PlanValidated"
	EventRoutingPlanCreated          EventType = "RoutingPlanCreated"
	EventTaskCreated                 EventType = "TaskCreated"
	EventTaskDispatched              EventType = "TaskDispatched"
	EventEnvelopeAcked               EventType = "EnvelopeAcked"
	EventEnvelopeDeadLettered        EventType = "EnvelopeDeadLettered"
	EventTaskExecutionAcquired       EventType = "TaskExecutionAcquired"
	EventTaskExecutionHeartbeat      EventType = "TaskExecutionHeartbeat"
	EventTaskExecutionReleased       EventType = "TaskExecutionReleased"
	EventTypedReportSubmitted        EventType = "TypedReportSubmitted"
	EventTaskCompleted               EventType = "TaskCompleted"
	EventTaskFailed                  EventType = "TaskFailed"
	EventTaskBlocked                 EventType = "TaskBlocked"
	EventTaskPaused                  EventType = "TaskPaused"
	EventUserInputSubmitted          EventType = "UserInputSubmitted"
	EventResumeTokenCreated          EventType = "ResumeTokenCreated"
	EventActionReconcileRequired     EventType = "ActionReconcileRequired"
	EventActionAttemptStarted        EventType = "ActionAttemptStarted"
	EventActionAttemptUpdated        EventType = "ActionAttemptUpdated"
	EventBlackboardItemWritten       EventType = "BlackboardItemWritten"
	EventHandoffRequested            EventType = "HandoffRequested"
	EventHandoffApplied              EventType = "HandoffApplied"
	EventHandoffEnvelopeQueued       EventType = "HandoffEnvelopeQueued"
	EventApprovalRequested           EventType = "ApprovalRequested"
	EventApprovalDecided             EventType = "ApprovalDecided"
	EventTaskOwnerChanged            EventType = "TaskOwnerChanged"
	EventPolicyObligationFailed      EventType = "PolicyObligationFailed"
	EventResponseTaskCreated         EventType = "ResponseTaskCreated"
	EventSystemResponseBypassAudited EventType = "SystemResponseBypassAudited"
	EventUserMessageComposed         EventType = "UserMessageComposed"
	EventUserMessagePolicyChecked    EventType = "UserMessagePolicyChecked"
	EventUserMessageQueued           EventType = "UserMessageQueued"
	EventResponsePublished           EventType = "ResponsePublished"
	EventResponsePublishFailed       EventType = "ResponsePublishFailed"
	EventTaskMonitorDecision         EventType = "TaskMonitorDecision"
	EventMailboxRetryScheduled       EventType = "MailboxRetryScheduled"
	EventTraceSpanStarted            EventType = "TraceSpanStarted"
	EventTraceSpanEnded              EventType = "TraceSpanEnded"
)

type Event struct {
	RunID      string         `json:"runId"`
	TaskID     string         `json:"taskId,omitempty"`
	Sequence   int            `json:"sequence"`
	Type       EventType      `json:"type"`
	Payload    map[string]any `json:"payload,omitempty"`
	RecordedAt time.Time      `json:"recordedAt"`
}
