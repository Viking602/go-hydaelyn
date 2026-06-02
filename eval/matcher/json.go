package matcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// jsonContains matches when the observed JSON value contains every key/value
// in a partial expectation (deep, structural containment).
type jsonContains struct {
	partial any
	err     error
}

// JSONContains returns a Matcher that passes when the observed value contains
// the partial expectation: every property in a partial object must be present
// and (recursively) contained in the observed object, and every element of a
// partial array must be contained somewhere in the observed array. Scalars
// must be equal. The observed value may be a Go value, a JSON string, or a
// []byte of JSON; the partial is normalized the same way.
//
// This is the comparator behind assertions that check a tool was called with a
// particular argument subset without pinning the whole payload.
func JSONContains(partial any) Matcher {
	normalized, err := normalizeJSON(partial)
	return jsonContains{partial: normalized, err: err}
}

// Match reports whether the observed value contains the partial expectation.
func (m jsonContains) Match(actual any) (bool, string) {
	if m.err != nil {
		return false, fmt.Sprintf("invalid partial value: %v", m.err)
	}
	value, err := normalizeJSON(actual)
	if err != nil {
		return false, fmt.Sprintf("observed value is not valid JSON: %v", err)
	}
	if path, ok := jsonContained(value, m.partial); !ok {
		return false, fmt.Sprintf("value does not contain expected partial at %s", path)
	}
	return true, ""
}

// jsonMatchSchema matches when the observed JSON value validates against the
// supported JSON Schema keyword subset.
type jsonMatchSchema struct {
	schema schemaNode
	err    error
}

// JSONMatchSchema returns a Matcher that passes when the observed value
// validates against schema. The supported keyword subset mirrors the agent
// loop's OutputPolicy validator: type (object/array/string/number/integer/
// boolean), properties, required, items, enum, and additionalProperties. An
// unsupported keyword or malformed schema is reported as a mismatch rather
// than panicking.
func JSONMatchSchema(schema json.RawMessage) Matcher {
	node, err := parseSchema(schema)
	return jsonMatchSchema{schema: node, err: err}
}

// Match reports whether the observed value validates against the schema.
func (m jsonMatchSchema) Match(actual any) (bool, string) {
	if m.err != nil {
		return false, fmt.Sprintf("invalid schema: %v", m.err)
	}
	value, err := normalizeJSON(actual)
	if err != nil {
		return false, fmt.Sprintf("observed value is not valid JSON: %v", err)
	}
	if err := validateAgainstSchema(value, m.schema, "$"); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// normalizeJSON folds an observed value into a canonical JSON value tree
// (map[string]any / []any / json.Number / string / bool / nil) so the
// comparators operate on a single representation regardless of whether the
// caller supplied a Go value, a JSON string, or a []byte of JSON.
func normalizeJSON(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return decodeJSON(v)
	case []byte:
		return decodeJSON(v)
	case string:
		trimmed := strings.TrimSpace(v)
		if looksLikeJSON(trimmed) {
			if decoded, err := decodeJSON([]byte(trimmed)); err == nil {
				return decoded, nil
			}
		}
		return v, nil
	default:
		// Round-trip arbitrary Go values through JSON so numbers land as
		// json.Number and maps/slices land in the canonical shape.
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return decodeJSON(raw)
	}
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func looksLikeJSON(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '{', '[', '"', 't', 'f', 'n', '-':
		return true
	}
	return s[0] >= '0' && s[0] <= '9'
}

// jsonContained reports whether actual structurally contains partial, returning
// the JSON path of the first mismatch when it does not.
func jsonContained(actual any, partial any) (string, bool) {
	return containedAt(actual, partial, "$")
}

func containedAt(actual any, partial any, path string) (string, bool) {
	switch want := partial.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return path, false
		}
		for key, wantValue := range want {
			gotValue, ok := got[key]
			if !ok {
				return childPath(path, key), false
			}
			if mismatch, ok := containedAt(gotValue, wantValue, childPath(path, key)); !ok {
				return mismatch, false
			}
		}
		return "", true
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return path, false
		}
		for index, wantValue := range want {
			if !sliceContains(got, wantValue) {
				return fmt.Sprintf("%s[%d]", path, index), false
			}
		}
		return "", true
	default:
		if equalJSON(actual, partial) {
			return "", true
		}
		return path, false
	}
}

func sliceContains(slice []any, want any) bool {
	for _, candidate := range slice {
		if _, ok := containedAt(candidate, want, "$"); ok {
			return true
		}
	}
	return false
}

func childPath(path string, key string) string {
	return path + "." + key
}

// equalJSON reports deep equality between two normalized JSON values, treating
// numbers by their numeric value rather than their textual form.
func equalJSON(left any, right any) bool {
	switch leftValue := left.(type) {
	case nil:
		return right == nil
	case string:
		rightValue, ok := right.(string)
		return ok && leftValue == rightValue
	case bool:
		rightValue, ok := right.(bool)
		return ok && leftValue == rightValue
	case json.Number:
		rightValue, ok := right.(json.Number)
		return ok && equalNumber(leftValue, rightValue)
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index := range leftValue {
			if !equalJSON(leftValue[index], rightValue[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, nested := range leftValue {
			other, ok := rightValue[key]
			if !ok || !equalJSON(nested, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func equalNumber(left json.Number, right json.Number) bool {
	leftRat, ok := new(big.Rat).SetString(left.String())
	if !ok {
		return false
	}
	rightRat, ok := new(big.Rat).SetString(right.String())
	return ok && leftRat.Cmp(rightRat) == 0
}
