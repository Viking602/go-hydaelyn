package run

import "time"

type RunnerOptions struct {
	Workspace  string
	OutputRoot string
	Now        func() time.Time
}

func (o RunnerOptions) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}
