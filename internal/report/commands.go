package report

import "github.com/Viking602/go-hydaelyn/internal/core/model"

type SubmitTypedCommand struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  model.HolderType
	HolderID    string
	TaskVersion int
	Report      model.TypedReport
}

func (SubmitTypedCommand) CommandName() string { return "report.submit_typed" }
