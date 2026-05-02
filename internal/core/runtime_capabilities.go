package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

// IDGenerator is the runtime capability required by handlers that create model IDs.
type IDGenerator func(prefix string) string

// UoWAuthorizer is the runtime policy boundary exposed to migrated handlers.
type UoWAuthorizer func(context.Context, ports.UnitOfWork, PolicyRequest) (PolicyDecision, error)

// UoWTraceRecorder records a completed trace span inside the active UnitOfWork.
type UoWTraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

// TaskMonitorProvider returns the currently configured task monitor.
type TaskMonitorProvider func() TaskMonitor

// PipelineProvider returns the currently configured pipeline components.
type PipelineProvider func() PipelineComponents

// OutputGatewayProvider returns the currently configured output gateway.
type OutputGatewayProvider func() OutputGateway

// ApprovalFactory creates an approval request and matching resume token for a task.
type ApprovalFactory func(Task, string, string) (ApprovalRequest, ResumeToken)
