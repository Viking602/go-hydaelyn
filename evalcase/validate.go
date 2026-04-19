package evalcase

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func ValidateCase(c evaluation.EvalCase) error {
	if c.SchemaVersion != evaluation.EvalCaseSchemaVersion {
		return fmt.Errorf("unsupported eval case schema version %q", c.SchemaVersion)
	}
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("eval case id is required")
	}
	if strings.TrimSpace(c.Suite) == "" {
		return fmt.Errorf("eval case suite is required")
	}
	if strings.TrimSpace(c.Pattern) == "" {
		return fmt.Errorf("eval case pattern is required")
	}
	for _, name := range c.Tools {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("eval case tools cannot contain empty names")
		}
	}
	if c.Profiles != nil {
		if strings.TrimSpace(c.Profiles.Supervisor) == "" {
			return fmt.Errorf("eval case supervisor profile is required when profiles are set")
		}
		if strings.TrimSpace(c.Profiles.Worker) == "" {
			return fmt.Errorf("eval case worker profile is required when profiles are set")
		}
	}
	if c.Fixtures != nil {
		for _, id := range c.Fixtures.CorpusIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("eval case fixture corpus ids cannot contain empty values")
			}
		}
		for _, path := range c.Fixtures.Paths {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("eval case fixture paths cannot contain empty values")
			}
		}
	}
	if c.Expected != nil {
		for _, citation := range c.Expected.RequiredCitations {
			if strings.TrimSpace(citation) == "" {
				return fmt.Errorf("eval case required citations cannot contain empty values")
			}
		}
	}
	if c.Thresholds != nil {
		if err := validateUnitInterval("taskCompletionRate", c.Thresholds.TaskCompletionRate); err != nil {
			return err
		}
		if err := validateUnitInterval("groundedness", c.Thresholds.Groundedness); err != nil {
			return err
		}
		if err := validateUnitInterval("supportedClaimRatio", c.Thresholds.SupportedClaimRatio); err != nil {
			return err
		}
		if err := validateUnitInterval("retrySuccessRate", c.Thresholds.RetrySuccessRate); err != nil {
			return err
		}
	}
	if c.Limits != nil {
		if c.Limits.MaxToolCalls < 0 || c.Limits.MaxLatencyMs < 0 || c.Limits.MaxTokens < 0 {
			return fmt.Errorf("eval case limits cannot be negative")
		}
	}
	return nil
}

func validateUnitInterval(name string, value float64) error {
	if value < 0 || value > 1 {
		return fmt.Errorf("eval case threshold %s must be between 0 and 1", name)
	}
	return nil
}
