package model

import "time"

type UserMessageStatus string

const (
	UserMessageComposed  UserMessageStatus = "composed"
	UserMessageQueued    UserMessageStatus = "queued"
	UserMessagePublished UserMessageStatus = "published"
	UserMessageFailed    UserMessageStatus = "failed"
	UserMessageCancelled UserMessageStatus = "cancelled"
)

type UserMessageType string

const (
	UserMessageTypeFinalAnswer          UserMessageType = "final_answer"
	UserMessageTypeProgressUpdate       UserMessageType = "progress_update"
	UserMessageTypeApprovalRequest      UserMessageType = "approval_request"
	UserMessageTypeClarificationRequest UserMessageType = "clarification_request"
	UserMessageTypeExecutionResult      UserMessageType = "execution_result"
	UserMessageTypeErrorNotice          UserMessageType = "error_notice"
	UserMessageTypeBlockedNotice        UserMessageType = "blocked_notice"
)

type UserMessage struct {
	ID             string            `json:"messageId"`
	RunID          string            `json:"runId"`
	TaskID         string            `json:"taskId"`
	Type           UserMessageType   `json:"type,omitempty"`
	Title          string            `json:"title,omitempty"`
	Payload        string            `json:"payload"`
	Status         UserMessageStatus `json:"status"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	PublishedAt    time.Time         `json:"publishedAt,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}
