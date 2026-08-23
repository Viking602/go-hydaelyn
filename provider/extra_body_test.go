package provider

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestValidateExtraBodyAcceptsJSONWireValues(t *testing.T) {
	body := map[string]any{
		"reasoning": map[string]any{"effort": "high"},
		"include":   []string{"reasoning.encrypted_content"},
		"raw":       json.RawMessage(`{"enabled":true}`),
		"limit":     12,
	}
	if err := ValidateExtraBody(body); err != nil {
		t.Fatalf("valid wire body rejected: %v", err)
	}
}

func TestValidateExtraBodyAppliesDepthLimitInsideRawJSON(t *testing.T) {
	raw := strings.Repeat("[", maxExtraBodyDepth+1) + "0" + strings.Repeat("]", maxExtraBodyDepth+1)
	err := ValidateExtraBody(map[string]any{"raw": json.RawMessage(raw)})
	if err == nil || !strings.Contains(err.Error(), "levels") {
		t.Fatalf("deep raw JSON error = %v", err)
	}
}

func TestValidateExtraBodyRejectsHostObjects(t *testing.T) {
	for name, value := range map[string]any{
		"callback": func() {},
		"channel":  make(chan struct{}),
		"pointer":  new(int),
		"struct":   struct{ Name string }{Name: "host"},
		"nan":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateExtraBody(map[string]any{"host": value})
			if err == nil || !strings.Contains(err.Error(), "provider extra body") {
				t.Fatalf("invalid host value error = %v", err)
			}
		})
	}
}
