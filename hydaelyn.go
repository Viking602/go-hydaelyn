package hydaelyn

import (
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/orchestrator"
	"github.com/Viking602/go-hydaelyn/team"
)

// New constructs the primary Run/Task orchestrator runtime.
func New(cfg Config) *Runtime { return orchestrator.NewRuntime(cfg) }

// NewOrchestrator constructs the primary Run/Task orchestrator runtime.
//
// Deprecated: use New.
func NewOrchestrator() *OrchestratorRuntime { return orchestrator.NewMemoryRuntime() }

// NewTeamRuntime constructs the legacy Team + Pattern runtime.
func NewTeamRuntime(cfg TeamConfig) *TeamRuntime { return host.New(cfg) }

// Public façade types. Each is a Go type alias for the equivalent type
// in a subpackage, so values constructed via either name are
// interchangeable.
type (
	Runtime                     = orchestrator.Runtime
	Config                      = orchestrator.Config
	OrchestratorRuntime         = orchestrator.Runtime
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
	HolderType                  = orchestrator.HolderType
	ReportStatus                = orchestrator.ReportStatus
	Flow                        = orchestrator.Flow
	PolicyRequest               = orchestrator.PolicyRequest
	PolicyDecision              = orchestrator.PolicyDecision
	UserMessage                 = orchestrator.UserMessage

	// Deprecated: use Runtime plus Run/Task APIs. This alias remains for
	// compatibility during the Team + Pattern migration window.
	TeamRuntime      = host.Runtime
	TeamConfig       = host.Config
	StartTeamRequest = host.StartTeamRequest

	Profile = team.Profile
	Role    = team.Role
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
)
