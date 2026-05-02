package userinput

type SubmitUserInputCommand struct {
	RunID  string
	TaskID string
	Input  string
}

func (SubmitUserInputCommand) CommandName() string { return "user_input.submit" }
