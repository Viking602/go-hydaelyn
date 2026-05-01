package model

type PolicyOperation string

const (
	PolicyOperationDispatch        PolicyOperation = "dispatch"
	PolicyOperationBlackboardRead  PolicyOperation = "blackboard_read"
	PolicyOperationBlackboardWrite PolicyOperation = "blackboard_write"
	PolicyOperationHandoff         PolicyOperation = "handoff"
	PolicyOperationToolCall        PolicyOperation = "tool_call"
	PolicyOperationAction          PolicyOperation = "action"
	PolicyOperationResponseCompose PolicyOperation = "response_compose"
	PolicyOperationResponsePublish PolicyOperation = "response_publish"
)

type PolicyRequest struct {
	Operation PolicyOperation     `json:"operation"`
	RunID     string              `json:"runId,omitempty"`
	TaskID    string              `json:"taskId,omitempty"`
	Actor     SourceIdentity      `json:"actor,omitempty"`
	Tool      *Tool               `json:"tool,omitempty"`
	Message   *UserMessage        `json:"message,omitempty"`
	Handoff   *HandoffRequest     `json:"handoff,omitempty"`
	Selector  *BlackboardSelector `json:"selector,omitempty"`
	Item      *BlackboardItem     `json:"item,omitempty"`
	Action    *ActionAttempt      `json:"action,omitempty"`
	Metadata  map[string]string   `json:"metadata,omitempty"`
}
