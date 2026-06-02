package matcher_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/eval/matcher"
)

func TestContainsSubstring_Match(t *testing.T) {
	cases := []struct {
		name   string
		m      matcher.Matcher
		actual any
		want   bool
	}{
		{"hit", matcher.ContainsSubstring("lo wo"), "hello world", true},
		{"miss", matcher.ContainsSubstring("xyz"), "hello world", false},
		{"case-sensitive miss", matcher.ContainsSubstring("HELLO"), "hello", false},
		{"fold hit", matcher.ContainsSubstringFold("HELLO"), "hello", true},
		{"non-string rendered", matcher.ContainsSubstring("42"), 42, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := tc.m.Match(tc.actual)
			if ok != tc.want {
				t.Fatalf("Match() = %v (%s), want %v", ok, detail, tc.want)
			}
			if !ok && detail == "" {
				t.Fatalf("mismatch should carry a detail")
			}
		})
	}
}

func TestRegexMatch_Match(t *testing.T) {
	if ok, _ := matcher.RegexMatch(`^h.*d$`).Match("hello world"); !ok {
		t.Fatalf("expected regex to match")
	}
	if ok, _ := matcher.RegexMatch(`^z`).Match("hello"); ok {
		t.Fatalf("expected regex not to match")
	}
	if ok, detail := matcher.RegexMatch(`(`).Match("hello"); ok || detail == "" {
		t.Fatalf("invalid regex should fail with detail, got ok=%v detail=%q", ok, detail)
	}
}

func TestJSONContains_Match(t *testing.T) {
	cases := []struct {
		name    string
		partial any
		actual  any
		want    bool
	}{
		{"object subset", map[string]any{"city": "paris"}, `{"city":"paris","country":"fr"}`, true},
		{"nested subset", map[string]any{"loc": map[string]any{"city": "paris"}}, `{"loc":{"city":"paris","lat":48}}`, true},
		{"missing key", map[string]any{"zip": "75001"}, `{"city":"paris"}`, false},
		{"array element contained", []any{map[string]any{"id": 2}}, `{}`, false},
		{"number equality", map[string]any{"n": 3}, `{"n":3.0}`, true},
		{"string raw partial", `{"a":1}`, `{"a":1,"b":2}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := matcher.JSONContains(tc.partial).Match(tc.actual)
			if ok != tc.want {
				t.Fatalf("Match() = %v (%s), want %v", ok, detail, tc.want)
			}
		})
	}
}

func TestJSONContains_ArrayContainment(t *testing.T) {
	m := matcher.JSONContains([]any{map[string]any{"id": 2}})
	if ok, detail := m.Match(`[{"id":1},{"id":2,"name":"x"}]`); !ok {
		t.Fatalf("expected array containment, got %s", detail)
	}
	if ok, _ := m.Match(`[{"id":1}]`); ok {
		t.Fatalf("expected array containment to fail")
	}
}

func TestJSONMatchSchema_Match(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["status"],"properties":{"status":{"type":"string"},"count":{"type":"integer"}}}`)
	m := matcher.JSONMatchSchema(schema)
	if ok, detail := m.Match(`{"status":"done","count":3}`); !ok {
		t.Fatalf("expected schema match, got %s", detail)
	}
	if ok, _ := m.Match(`{"count":3}`); ok {
		t.Fatalf("expected missing-required to fail")
	}
	if ok, _ := m.Match(`{"status":"done","count":"x"}`); ok {
		t.Fatalf("expected wrong-type to fail")
	}
}

func TestJSONMatchSchema_UnsupportedKeyword(t *testing.T) {
	m := matcher.JSONMatchSchema(json.RawMessage(`{"type":"object","minProperties":1}`))
	if ok, detail := m.Match(`{}`); ok || detail == "" {
		t.Fatalf("unsupported keyword should fail with detail, got ok=%v detail=%q", ok, detail)
	}
}

func TestJSONMatchSchema_EnumAndArray(t *testing.T) {
	schema := json.RawMessage(`{"type":"array","items":{"type":"string","enum":["a","b"]}}`)
	m := matcher.JSONMatchSchema(schema)
	if ok, detail := m.Match(`["a","b","a"]`); !ok {
		t.Fatalf("expected enum/array match, got %s", detail)
	}
	if ok, _ := m.Match(`["a","c"]`); ok {
		t.Fatalf("expected out-of-enum to fail")
	}
}

// stubEmbedding maps known phrases to fixed vectors so cosine similarity is
// deterministic; unknown text embeds to a fixed orthogonal vector.
type stubEmbedding struct {
	fail bool
}

func (s stubEmbedding) Embed(text string) ([]float64, error) {
	if s.fail {
		return nil, errors.New("boom")
	}
	switch text {
	case "the cat sat", "a cat is sitting":
		return []float64{1, 1, 0}, nil
	case "the dog ran":
		return []float64{0, 0, 1}, nil
	default:
		return []float64{1, 0, 0}, nil
	}
}

func TestEmbeddingSimilarity_Match(t *testing.T) {
	m := matcher.EmbeddingSimilarity("the cat sat", 0.9, stubEmbedding{})
	if ok, detail := m.Match("a cat is sitting"); !ok {
		t.Fatalf("expected similar vectors to pass, got %s", detail)
	}
	if ok, _ := m.Match("the dog ran"); ok {
		t.Fatalf("expected orthogonal vectors to fail threshold")
	}
}

func TestEmbeddingSimilarity_NilProvider(t *testing.T) {
	if ok, detail := matcher.EmbeddingSimilarity("x", 0.5, nil).Match("y"); ok || detail == "" {
		t.Fatalf("nil provider should fail with detail, got ok=%v detail=%q", ok, detail)
	}
}

func TestEmbeddingSimilarity_ProviderError(t *testing.T) {
	if ok, detail := matcher.EmbeddingSimilarity("x", 0.5, stubEmbedding{fail: true}).Match("y"); ok || detail == "" {
		t.Fatalf("provider error should fail with detail, got ok=%v detail=%q", ok, detail)
	}
}
