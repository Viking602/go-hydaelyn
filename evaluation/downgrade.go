package evaluation

type downgradeRule struct {
	triggered bool
	cap       ScoreLevel
}

func ApplyHardDowngradeRules(score *ScorePayload) ScoreLevel {
	if score == nil {
		return ScoreLevelA0
	}
	level := score.Level
	if level == "" {
		level = ScoreLevelForOverallScoreWithReplayConsistency(score.OverallScore, score.ReplayConsistent)
	}
	for _, rule := range hardDowngradeRules(score) {
		if !rule.triggered {
			continue
		}
		level = minScoreLevel(level, rule.cap)
	}
	return level
}

func hardDowngradeRules(score *ScorePayload) []downgradeRule {
	return []downgradeRule{
		{triggered: score.SafetyMetrics != nil && score.SafetyMetrics.CriticalFailure, cap: ScoreLevelA1},
		{triggered: !score.ReplayConsistent, cap: ScoreLevelA2},
		{triggered: score.RuntimeMetrics != nil && score.RuntimeMetrics.TaskCompletionRate < 0.80, cap: ScoreLevelA2},
		{triggered: score.QualityMetrics != nil && score.QualityMetrics.Groundedness < 0.70, cap: ScoreLevelA2},
		{triggered: score.QualityMetrics != nil && score.QualityMetrics.SynthesisInputCoverage < 0.80, cap: ScoreLevelA2},
	}
}

func minScoreLevel(current, cap ScoreLevel) ScoreLevel {
	if scoreLevelRank(current) <= scoreLevelRank(cap) {
		return current
	}
	return cap
}

func scoreLevelRank(level ScoreLevel) int {
	switch level {
	case ScoreLevelA4:
		return 4
	case ScoreLevelA3:
		return 3
	case ScoreLevelA2:
		return 2
	case ScoreLevelA1:
		return 1
	default:
		return 0
	}
}
