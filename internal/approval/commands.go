package approval

type RequestApprovalCommand struct {
	RunID            string
	TaskID           string
	ActionID         string
	RequesterAgentID string
	Reason           string
	RiskSummary      string
	RequestedAction  string
}

type DecideApprovalCommand struct {
	RunID      string
	ApprovalID string
	DecidedBy  string
	Decision   string
	Reason     string
}

type RecoverResumeTokenCommand struct {
	TokenID string
}

func (RequestApprovalCommand) CommandName() string    { return "approval.request" }
func (DecideApprovalCommand) CommandName() string     { return "approval.decide" }
func (RecoverResumeTokenCommand) CommandName() string { return "resume_token.recover" }
