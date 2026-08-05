package adapter

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
)

func ResourceClaimSpecToModel(in api.ResourceClaimSpec) model.ResourceClaimSpec {
	return model.ResourceClaimSpec{ID: in.ID, Key: in.Key, Mode: model.ResourceClaimMode(in.Mode)}
}

func ResourceClaimSpecFromModel(in model.ResourceClaimSpec) api.ResourceClaimSpec {
	return api.ResourceClaimSpec{ID: in.ID, Key: in.Key, Mode: api.ResourceClaimMode(in.Mode)}
}

func ResourceClaimSpecsToModel(in []api.ResourceClaimSpec) []model.ResourceClaimSpec {
	if in == nil {
		return nil
	}
	out := make([]model.ResourceClaimSpec, len(in))
	for index, claim := range in {
		out[index] = ResourceClaimSpecToModel(claim)
	}
	return out
}

func ResourceClaimSpecsFromModel(in []model.ResourceClaimSpec) []api.ResourceClaimSpec {
	if in == nil {
		return nil
	}
	out := make([]api.ResourceClaimSpec, len(in))
	for index, claim := range in {
		out[index] = ResourceClaimSpecFromModel(claim)
	}
	return out
}

func ResourceClaimToModel(in api.ResourceClaim) model.ResourceClaim {
	return model.ResourceClaim{
		ID: in.ID, Key: in.Key, Mode: model.ResourceClaimMode(in.Mode), RunID: in.RunID,
		TaskID: in.TaskID, LeaseID: in.LeaseID, HolderID: in.HolderID,
		State: model.ResourceClaimState(in.State), Version: in.Version,
		CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimFromModel(in model.ResourceClaim) api.ResourceClaim {
	return api.ResourceClaim{
		ID: in.ID, Key: in.Key, Mode: api.ResourceClaimMode(in.Mode), RunID: in.RunID,
		TaskID: in.TaskID, LeaseID: in.LeaseID, HolderID: in.HolderID,
		State: api.ResourceClaimState(in.State), Version: in.Version,
		CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimsToModel(in []api.ResourceClaim) []model.ResourceClaim {
	if in == nil {
		return nil
	}
	out := make([]model.ResourceClaim, len(in))
	for index, claim := range in {
		out[index] = ResourceClaimToModel(claim)
	}
	return out
}

func ResourceClaimsFromModel(in []model.ResourceClaim) []api.ResourceClaim {
	if in == nil {
		return nil
	}
	out := make([]api.ResourceClaim, len(in))
	for index, claim := range in {
		out[index] = ResourceClaimFromModel(claim)
	}
	return out
}

func ResourceClaimRequestToModel(in api.ResourceClaimRequest) model.ResourceClaimRequest {
	return model.ResourceClaimRequest{
		RunID: in.RunID, TaskID: in.TaskID, LeaseID: in.LeaseID, HolderID: in.HolderID,
		Claims: ResourceClaimSpecsToModel(in.Claims), RequestedAt: in.RequestedAt, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimRequestFromModel(in model.ResourceClaimRequest) api.ResourceClaimRequest {
	return api.ResourceClaimRequest{
		RunID: in.RunID, TaskID: in.TaskID, LeaseID: in.LeaseID, HolderID: in.HolderID,
		Claims: ResourceClaimSpecsFromModel(in.Claims), RequestedAt: in.RequestedAt, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimTransitionToModel(in api.ResourceClaimTransition) model.ResourceClaimTransition {
	return model.ResourceClaimTransition{
		ClaimID: in.ClaimID, ExpectedVersion: in.ExpectedVersion,
		To: model.ResourceClaimState(in.To), At: in.At, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimTransitionFromModel(in model.ResourceClaimTransition) api.ResourceClaimTransition {
	return api.ResourceClaimTransition{
		ClaimID: in.ClaimID, ExpectedVersion: in.ExpectedVersion,
		To: api.ResourceClaimState(in.To), At: in.At, ExpiresAt: in.ExpiresAt,
	}
}

func ResourceClaimTransitionRequestToModel(in api.ResourceClaimTransitionRequest) model.ResourceClaimTransitionRequest {
	out := model.ResourceClaimTransitionRequest{Transitions: make([]model.ResourceClaimTransition, len(in.Transitions))}
	for index, transition := range in.Transitions {
		out.Transitions[index] = ResourceClaimTransitionToModel(transition)
	}
	return out
}

func ResourceClaimTransitionRequestFromModel(in model.ResourceClaimTransitionRequest) api.ResourceClaimTransitionRequest {
	out := api.ResourceClaimTransitionRequest{Transitions: make([]api.ResourceClaimTransition, len(in.Transitions))}
	for index, transition := range in.Transitions {
		out.Transitions[index] = ResourceClaimTransitionFromModel(transition)
	}
	return out
}

func ResourceClaimDecisionToModel(in api.ResourceClaimDecision) model.ResourceClaimDecision {
	return model.ResourceClaimDecision{
		Acquired: in.Acquired, Reason: model.ResourceClaimDenialReason(in.Reason),
		Claims: ResourceClaimsToModel(in.Claims), Conflicts: ResourceClaimsToModel(in.Conflicts),
	}
}

func ResourceClaimDecisionFromModel(in model.ResourceClaimDecision) api.ResourceClaimDecision {
	return api.ResourceClaimDecision{
		Acquired: in.Acquired, Reason: api.ResourceClaimDenialReason(in.Reason),
		Claims: ResourceClaimsFromModel(in.Claims), Conflicts: ResourceClaimsFromModel(in.Conflicts),
	}
}

func ResourceClaimSelectorToModel(in api.ResourceClaimSelector) model.ResourceClaimSelector {
	out := model.ResourceClaimSelector{
		IDs: cloneStrings(in.IDs), Keys: cloneStrings(in.Keys), RunIDs: cloneStrings(in.RunIDs),
		TaskIDs: cloneStrings(in.TaskIDs), LeaseIDs: cloneStrings(in.LeaseIDs), HolderIDs: cloneStrings(in.HolderIDs),
		ExpiresBefore: in.ExpiresBefore, Limit: in.Limit,
	}
	out.Modes = make([]model.ResourceClaimMode, len(in.Modes))
	for index, mode := range in.Modes {
		out.Modes[index] = model.ResourceClaimMode(mode)
	}
	out.States = make([]model.ResourceClaimState, len(in.States))
	for index, state := range in.States {
		out.States[index] = model.ResourceClaimState(state)
	}
	return out
}

func ResourceClaimSelectorFromModel(in model.ResourceClaimSelector) api.ResourceClaimSelector {
	out := api.ResourceClaimSelector{
		IDs: cloneStrings(in.IDs), Keys: cloneStrings(in.Keys), RunIDs: cloneStrings(in.RunIDs),
		TaskIDs: cloneStrings(in.TaskIDs), LeaseIDs: cloneStrings(in.LeaseIDs), HolderIDs: cloneStrings(in.HolderIDs),
		ExpiresBefore: in.ExpiresBefore, Limit: in.Limit,
	}
	out.Modes = make([]api.ResourceClaimMode, len(in.Modes))
	for index, mode := range in.Modes {
		out.Modes[index] = api.ResourceClaimMode(mode)
	}
	out.States = make([]api.ResourceClaimState, len(in.States))
	for index, state := range in.States {
		out.States[index] = api.ResourceClaimState(state)
	}
	return out
}
