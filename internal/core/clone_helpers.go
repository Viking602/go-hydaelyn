package core

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/eventpayload"
)

func taskEventPayload(task api.Task) map[string]any {
	return eventpayload.Task(task)
}

func runPayload(run api.Run) map[string]any {
	return eventpayload.Run(run)
}
