package host

import (
	"strings"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

var publishPipeline = blackboard.NewPipeline()

func (r *Runtime) applyBlackboardUpdate(state team.RunState, task team.Task) team.RunState {
	if task.Result == nil {
		return state
	}
	if state.Blackboard == nil {
		state.Blackboard = &blackboard.State{}
	}
	switch task.Kind {
	case team.TaskKindResearch:
		request := blackboard.PublishRequest{
			TaskID:     task.ID,
			Title:      task.Title,
			Summary:    task.Result.Summary,
			Confidence: task.Result.Confidence,
			Evidence:   evidenceInputs(task.Result.Evidence),
		}
		publishPipeline.Publish(state.Blackboard, request)
	case team.TaskKindVerify:
		status := blackboard.InferVerificationStatus(task.Result.Summary)
		confidence := task.Result.Confidence
		if confidence <= 0 {
			confidence = 0.75
		}
		for _, dependencyID := range task.DependsOn {
			for _, claim := range state.Blackboard.ClaimsForTask(dependencyID) {
				state.Blackboard.UpsertVerification(blackboard.VerificationResult{
					ClaimID:    claim.ID,
					Status:     status,
					Confidence: confidence,
					Rationale:  strings.TrimSpace(task.Result.Summary),
				})
			}
		}
	}
	return state
}

func evidenceInputs(items []team.Evidence) []blackboard.EvidenceInput {
	result := make([]blackboard.EvidenceInput, 0, len(items))
	for _, item := range items {
		result = append(result, blackboard.EvidenceInput{
			Source:  item.Source,
			Snippet: item.Snippet,
		})
	}
	return result
}
