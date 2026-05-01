package core

type SubmitResponseOutputCommand struct {
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     HolderType
	HolderID       string
	TaskVersion    int
	Type           UserMessageType
	Title          string
	Payload        string
	IdempotencyKey string
}

type PublishResponseCommand struct {
	RunID     string
	MessageID string
}
