package core

import (
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/eventpayload"
)

func taskEventPayload(task model.Task) map[string]any {
	return eventpayload.Task(task)
}

func runPayload(run model.Run) map[string]any {
	return eventpayload.Run(run)
}
