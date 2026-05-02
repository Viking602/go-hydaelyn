package trace

type StartTraceSpanCommand struct {
	RunID     string
	TaskID    string
	TraceID   string
	ParentID  string
	Name      string
	Component string
	Metadata  map[string]string
}

type EndTraceSpanCommand struct {
	SpanID string
	Error  string
}

func (StartTraceSpanCommand) CommandName() string { return "trace.start" }
func (EndTraceSpanCommand) CommandName() string   { return "trace.end" }
