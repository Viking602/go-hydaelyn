package core

import response "github.com/Viking602/go-hydaelyn/internal/response"

func redactUserPayload(payload string) string {
	return response.RedactUserPayload(payload)
}

func redactEmail(payload string) string {
	return response.RedactEmail(payload)
}

func hideInternalTrace(payload string) string {
	return response.HideInternalTrace(payload)
}
