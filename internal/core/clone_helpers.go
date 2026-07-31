package core

import (
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/eventpayload"
)

func taskEventPayload(task model.Task) map[string]any {
	return eventpayload.Task(task)
}

func runPayload(run model.Run) map[string]any {
	return eventpayload.Run(run)
}
