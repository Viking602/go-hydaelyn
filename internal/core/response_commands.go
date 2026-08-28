package core

import responsesvc "github.com/Viking602/venat/internal/response"

type (
	SubmitResponseOutputCommand         = responsesvc.SubmitOutputCommand
	PublishResponseCommand              = responsesvc.PublishCommand
	ReconcileResponsePublicationCommand = responsesvc.ReconcilePublicationCommand
)

type reconcileResponsePublicationResult = responsesvc.ReconcilePublicationResult
