// Package hydaelyn is the public façade for the Hydaelyn orchestrator runtime.
//
// All exported names re-route to the orchestrator package; legacy host/team
// runtimes were removed in v2.0.
package hydaelyn

import "github.com/Viking602/go-hydaelyn/orchestrator"

// New constructs the primary Run/Task orchestrator runtime.
func New(cfg Config) *Runtime { return orchestrator.NewRuntime(cfg) }

// Public façade types. Each is a Go type alias for the equivalent type in the
// orchestrator package, so values constructed via either name are
// interchangeable.
type (
	Runtime                     = orchestrator.Runtime
	Config                      = orchestrator.Config
	Run                         = orchestrator.Run
	Task                        = orchestrator.Task
	RunStatus                   = orchestrator.RunStatus
	TaskStatus                  = orchestrator.TaskStatus
	TaskType                    = orchestrator.TaskType
	StartRunCommand             = orchestrator.StartRunCommand
	CreateTaskCommand           = orchestrator.CreateTaskCommand
	AdvanceRunCommand           = orchestrator.AdvanceRunCommand
	DispatchTaskCommand         = orchestrator.DispatchTaskCommand
	AcquireTaskExecutionCommand = orchestrator.AcquireTaskExecutionCommand
	SubmitTypedReportCommand    = orchestrator.SubmitTypedReportCommand
	SubmitResponseOutputCommand = orchestrator.SubmitResponseOutputCommand
	PublishResponseCommand      = orchestrator.PublishResponseCommand
	HandoffCommand              = orchestrator.HandoffCommand
	RunTimelineItem             = orchestrator.RunTimelineItem
	TypedReport                 = orchestrator.TypedReport
	TaskExecutionLease          = orchestrator.TaskExecutionLease
	TaskEnvelope                = orchestrator.TaskEnvelope
	Tool                        = orchestrator.Tool
	ToolEffectType              = orchestrator.ToolEffectType
	RetryPolicy                 = orchestrator.RetryPolicy
	HolderType                  = orchestrator.HolderType
	ReportStatus                = orchestrator.ReportStatus
	Flow                        = orchestrator.Flow
	PolicyEngine                = orchestrator.PolicyEngine
	PolicyRequest               = orchestrator.PolicyRequest
	PolicyDecision              = orchestrator.PolicyDecision
	OutputGateway               = orchestrator.OutputGateway
	PipelineComponents          = orchestrator.PipelineComponents
	UserMessage                 = orchestrator.UserMessage
	UserMessageType             = orchestrator.UserMessageType
)

const (
	HolderAgent     = orchestrator.HolderAgent
	HolderComponent = orchestrator.HolderComponent

	ReportStatusSuccess            = orchestrator.ReportStatusSuccess
	ReportStatusPartialSuccess     = orchestrator.ReportStatusPartialSuccess
	ReportStatusFailed             = orchestrator.ReportStatusFailed
	ReportStatusBlocked            = orchestrator.ReportStatusBlocked
	ReportStatusNeedsHandoff       = orchestrator.ReportStatusNeedsHandoff
	ReportStatusNeedsApproval      = orchestrator.ReportStatusNeedsApproval
	ReportStatusNeedsClarification = orchestrator.ReportStatusNeedsClarification

	ToolEffectReadOnly           = orchestrator.ToolEffectReadOnly
	ToolEffectWrite              = orchestrator.ToolEffectWrite
	ToolEffectExternalSideEffect = orchestrator.ToolEffectExternalSideEffect

	UserMessageTypeFinalAnswer          = orchestrator.UserMessageTypeFinalAnswer
	UserMessageTypeProgressUpdate       = orchestrator.UserMessageTypeProgressUpdate
	UserMessageTypeApprovalRequest      = orchestrator.UserMessageTypeApprovalRequest
	UserMessageTypeClarificationRequest = orchestrator.UserMessageTypeClarificationRequest
	UserMessageTypeExecutionResult      = orchestrator.UserMessageTypeExecutionResult
	UserMessageTypeErrorNotice          = orchestrator.UserMessageTypeErrorNotice
	UserMessageTypeBlockedNotice        = orchestrator.UserMessageTypeBlockedNotice
)
