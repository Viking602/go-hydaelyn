package matcher

import (
	"fmt"
	"math"
)

// EmbeddingProvider turns text into a vector. It is the comparator's only
// dependency on the host: the framework ships the cosine-similarity comparator
// but not an embedding model. Applications inject a concrete provider — the
// eval.Harness exposes one via EmbeddingProvider(), and eval.EmbeddingProvider
// satisfies this interface structurally.
type EmbeddingProvider interface {
	// Embed returns the embedding vector for text.
	Embed(text string) ([]float64, error)
}

// embeddingSimilarity matches when the cosine similarity between the observed
// value and a reference text meets or exceeds a threshold.
type embeddingSimilarity struct {
	reference string
	threshold float64
	provider  EmbeddingProvider
}

// EmbeddingSimilarity returns a Matcher that passes when the cosine similarity
// between the embedding of the observed value and the embedding of reference is
// at least threshold (in [0,1]). The framework supplies only the comparator;
// callers inject the embedding provider, typically from
// eval.Harness.EmbeddingProvider(). A nil provider is reported as a mismatch so
// a misconfigured case fails rather than panics.
func EmbeddingSimilarity(reference string, threshold float64, provider EmbeddingProvider) Matcher {
	return embeddingSimilarity{reference: reference, threshold: threshold, provider: provider}
}

// Match embeds the observed value and the reference, then compares their cosine
// similarity against the threshold.
func (m embeddingSimilarity) Match(actual any) (bool, string) {
	if m.provider == nil {
		return false, "embedding similarity requires an embedding provider (Harness.EmbeddingProvider returned nil)"
	}
	text := renderText(actual)
	actualVec, err := m.provider.Embed(text)
	if err != nil {
		return false, fmt.Sprintf("embed observed value: %v", err)
	}
	referenceVec, err := m.provider.Embed(m.reference)
	if err != nil {
		return false, fmt.Sprintf("embed reference: %v", err)
	}
	similarity, err := cosineSimilarity(actualVec, referenceVec)
	if err != nil {
		return false, err.Error()
	}
	if similarity >= m.threshold {
		return true, ""
	}
	return false, fmt.Sprintf("embedding similarity %.4f is below threshold %.4f", similarity, m.threshold)
}

// cosineSimilarity computes the cosine similarity of two equal-length vectors.
func cosineSimilarity(a []float64, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions differ: %d vs %d", len(a), len(b))
	}
	if len(a) == 0 {
		return 0, fmt.Errorf("embedding vectors are empty")
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("embedding vector has zero magnitude")
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), nil
}
