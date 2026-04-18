package host

const (
	// eventReasonHeartbeatExpired marks lease-level expiration caused by worker loss
	// or failed heartbeats.
	eventReasonHeartbeatExpired = "heartbeat_expired"
	// eventReasonStateVersionConflict marks storage-level CAS/save conflicts caused
	// by stale state versions.
	eventReasonStateVersionConflict = "state_version_conflict"
	// eventReasonTaskAlreadyTerminal marks task-level attempts to persist or execute
	// results for a task that already reached a terminal state.
	eventReasonTaskAlreadyTerminal = "task_already_terminal"
	// eventReasonTeamAborted marks team-level cancellation triggered explicitly by
	// user or API abort requests.
	eventReasonTeamAborted = "team_aborted"
	// eventReasonSupersededByReplan marks planning-level replacement caused by a
	// planner replan.
	eventReasonSupersededByReplan = "superseded_by_replan"
)

var lifecycleReasonTaxonomy = map[string]struct{}{
	eventReasonHeartbeatExpired:     {},
	eventReasonStateVersionConflict: {},
	eventReasonTaskAlreadyTerminal:  {},
	eventReasonTeamAborted:          {},
	eventReasonSupersededByReplan:   {},
}

func normalizeLifecycleReason(reason string) string {
	if _, ok := lifecycleReasonTaxonomy[reason]; ok {
		return reason
	}
	return reason
}
