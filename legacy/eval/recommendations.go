package eval

import "sort"

func MapRecommendations(failures []ScoreFailure, score *ScorePayload) []ScoreRecommendation {
	if len(failures) == 0 {
		return nil
	}
	recommendations := make([]ScoreRecommendation, 0, len(failures))
	seen := map[string]struct{}{}
	for _, failure := range failures {
		recommendation := recommendationForFailure(failure, score)
		if recommendation.Action == "" {
			continue
		}
		key := recommendation.Priority + "|" + recommendation.Action
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		recommendations = append(recommendations, recommendation)
	}
	sort.SliceStable(recommendations, func(i, j int) bool {
		if recommendationPriorityRank(recommendations[i].Priority) != recommendationPriorityRank(recommendations[j].Priority) {
			return recommendationPriorityRank(recommendations[i].Priority) < recommendationPriorityRank(recommendations[j].Priority)
		}
		return recommendations[i].Action < recommendations[j].Action
	})
	return recommendations
}

func recommendationForFailure(failure ScoreFailure, score *ScorePayload) ScoreRecommendation {
	switch failure.Code {
	case "critical-safety-failure", "policy-blocked":
		return ScoreRecommendation{Priority: "P0", Action: "tighten safety policy gates before release", Rationale: failure.Message}
	case "replay-inconsistent":
		return ScoreRecommendation{Priority: "P1", Action: "fix replay serialization and ordering invariants", Rationale: failure.Message}
	case "low-task-completion", "blocking-failures", "task-failed":
		return ScoreRecommendation{Priority: "P1", Action: "improve planner decomposition and blocking failure recovery", Rationale: failure.Message}
	case "low-groundedness", "groundedness-threshold-miss", "low-citation-precision", "low-citation-recall":
		return ScoreRecommendation{Priority: "P1", Action: "strengthen grounding checks and citation enforcement", Rationale: failure.Message}
	case "low-synthesis-coverage":
		return ScoreRecommendation{Priority: "P1", Action: "ensure synthesis reads every required upstream artifact", Rationale: failure.Message}
	case "low-tool-precision", "low-tool-recall", "low-tool-arg-accuracy":
		return ScoreRecommendation{Priority: "P2", Action: "tighten tool selection and argument validation", Rationale: failure.Message}
	case "retry-success-threshold-miss":
		return ScoreRecommendation{Priority: "P2", Action: "review retry strategy for recoverable tool and model errors", Rationale: failure.Message}
	default:
		if failure.Layer == "runtime" && score != nil && !score.ReplayConsistent {
			return ScoreRecommendation{Priority: "P1", Action: "stabilize runtime event capture before broad rollout", Rationale: failure.Message}
		}
		return ScoreRecommendation{}
	}
}

func recommendationPriorityRank(priority string) int {
	switch priority {
	case "P0":
		return 0
	case "P1":
		return 1
	default:
		return 2
	}
}
