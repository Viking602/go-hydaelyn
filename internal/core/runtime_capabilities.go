package core

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

// IDGenerator is the runtime capability required by handlers that create model IDs.
type IDGenerator func(prefix string) string

// UoWAuthorizer is the runtime policy boundary exposed to migrated handlers.
type UoWAuthorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

// UoWTraceRecorder records a completed trace span inside the active UnitOfWork.
type UoWTraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

// TaskMonitorProvider returns the currently configured task monitor.
type TaskMonitorProvider func() TaskMonitor

// PipelineProvider returns the currently configured pipeline components.
type PipelineProvider func() PipelineComponents

// OutputGatewayProvider returns the currently configured output gateway.
type OutputGatewayProvider func() OutputGateway

// ApprovalFactory creates an approval request and matching resume token for a task.
type ApprovalFactory func(api.Task, string, string) (api.ApprovalRequest, api.ResumeToken)
