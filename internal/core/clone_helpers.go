package core

import "github.com/Viking602/go-hydaelyn/internal/eventpayload"

func taskEventPayload(task Task) map[string]any {
	return eventpayload.Task(task)
}

func runPayload(run Run) map[string]any {
	return eventpayload.Run(run)
}

func envPayload(env TaskEnvelope) map[string]any {
	return eventpayload.Envelope(env)
}

func retryPolicyPayload(policy RetryPolicy) map[string]any {
	return eventpayload.RetryPolicy(policy)
}

func cloneAnyMap(in map[string]any) map[string]any {
	return eventpayload.CloneAnyMap(in)
}
