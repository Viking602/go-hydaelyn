package core

type SubmitTypedReportCommand struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	Report      TypedReport
}

type SubmitUserInputCommand struct {
	RunID  string
	TaskID string
	Input  string
}
