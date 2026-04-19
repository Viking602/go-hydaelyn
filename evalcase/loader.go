package evalcase

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func LoadCase(path string) (evaluation.EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evaluation.EvalCase{}, fmt.Errorf("read eval case: %w", err)
	}
	var c evaluation.EvalCase
	if err := json.Unmarshal(data, &c); err != nil {
		return evaluation.EvalCase{}, fmt.Errorf("decode eval case: %w", err)
	}
	if err := ValidateCase(c); err != nil {
		return evaluation.EvalCase{}, err
	}
	return c, nil
}
