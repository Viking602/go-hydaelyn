package message

import (
	"bytes"
	"encoding/json"
)

// ContentKind identifies one ordered message or tool-result content block.
type ContentKind string

const (
	ContentText              ContentKind = "text"
	ContentCommentary        ContentKind = "commentary"
	ContentFinalAnswer       ContentKind = "final_answer"
	ContentReasoning         ContentKind = "reasoning"
	ContentRedactedReasoning ContentKind = "redacted_reasoning"
	ContentImage             ContentKind = "image"
	ContentAudio             ContentKind = "audio"
	ContentFile              ContentKind = "file"
	ContentSource            ContentKind = "source"
	ContentProviderData      ContentKind = "provider_data"
)

// Source identifies provider- or tool-supplied evidence without flattening it
// into assistant prose.
type Source struct {
	ID        string `json:"id,omitempty"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Filename  string `json:"filename,omitempty"`
}

// ContentPart preserves ordered text phases, binary modalities, citations,
// opaque provider blocks, and reasoning signatures on one neutral contract.
type ContentPart struct {
	ID           string          `json:"id,omitempty"`
	Kind         ContentKind     `json:"kind"`
	Text         string          `json:"text,omitempty"`
	Data         []byte          `json:"data,omitempty"`
	MediaType    string          `json:"mediaType,omitempty"`
	URI          string          `json:"uri,omitempty"`
	Filename     string          `json:"filename,omitempty"`
	Source       *Source         `json:"source,omitempty"`
	Signature    string          `json:"signature,omitempty"`
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

func TextPart(text string) ContentPart {
	return ContentPart{Kind: ContentText, Text: text}
}

func CommentaryPart(text string) ContentPart {
	return ContentPart{Kind: ContentCommentary, Text: text}
}

func FinalAnswerPart(text string) ContentPart {
	return ContentPart{Kind: ContentFinalAnswer, Text: text}
}

func ReasoningPart(text, signature string) ContentPart {
	return ContentPart{Kind: ContentReasoning, Text: text, Signature: signature}
}

func CloneContent(parts []ContentPart) []ContentPart {
	if parts == nil {
		return nil
	}
	cloned := make([]ContentPart, len(parts))
	for index, part := range parts {
		cloned[index] = part
		cloned[index].Data = append([]byte(nil), part.Data...)
		cloned[index].ProviderData = append(json.RawMessage(nil), part.ProviderData...)
		if part.Source != nil {
			source := *part.Source
			cloned[index].Source = &source
		}
	}
	return cloned
}

// CanonicalContent returns the ordered content contract. It promotes legacy
// scalar fields only when Content has not yet been populated.
func (m Message) CanonicalContent() []ContentPart {
	if len(m.Content) > 0 {
		return CloneContent(m.Content)
	}
	parts := make([]ContentPart, 0, 3)
	if m.Thinking != "" || m.ThinkingSignature != "" {
		parts = append(parts, ReasoningPart(m.Thinking, m.ThinkingSignature))
	}
	if m.RedactedThinking != "" {
		parts = append(parts, ContentPart{Kind: ContentRedactedReasoning, Data: []byte(m.RedactedThinking)})
	}
	if m.Text != "" {
		parts = append(parts, TextPart(m.Text))
	}
	return parts
}

func (m Message) TextContent() string {
	return textForKinds(m.CanonicalContent(), ContentText, ContentCommentary, ContentFinalAnswer)
}

func (m Message) FinalAnswerContent() string {
	return textForKinds(m.CanonicalContent(), ContentText, ContentFinalAnswer)
}

func (m Message) ReasoningContent() string {
	return textForKinds(m.CanonicalContent(), ContentReasoning)
}

func (m Message) HasContent() bool {
	return len(m.CanonicalContent()) > 0 || len(m.ToolCalls) > 0 || m.ToolResult != nil || len(m.ProviderState) > 0
}

// SyncLegacyContent promotes legacy scalar fields to Content, or derives those
// fields from canonical Content for callers that have not migrated yet. It is a
// transition boundary; new provider and persistence code should read Content.
func (m *Message) SyncLegacyContent() {
	if m == nil {
		return
	}
	if len(m.Content) == 0 {
		m.Content = m.CanonicalContent()
		return
	}
	m.Text = textForKinds(m.Content, ContentText, ContentCommentary, ContentFinalAnswer)
	m.Thinking = textForKinds(m.Content, ContentReasoning)
	m.ThinkingSignature = ""
	m.RedactedThinking = ""
	for _, part := range m.Content {
		if part.Kind == ContentReasoning && part.Signature != "" {
			m.ThinkingSignature = part.Signature
		}
		if part.Kind == ContentRedactedReasoning && len(part.Data) > 0 {
			m.RedactedThinking = string(part.Data)
		}
	}
}

func (r ToolResult) CanonicalContent() []ContentPart {
	if len(r.Parts) > 0 {
		return CloneContent(r.Parts)
	}
	if r.Content == "" {
		return nil
	}
	return []ContentPart{TextPart(r.Content)}
}

func (r ToolResult) TextContent() string {
	return textForKinds(r.CanonicalContent(), ContentText, ContentCommentary, ContentFinalAnswer)
}

func (r *ToolResult) SyncLegacyContent() {
	if r == nil {
		return
	}
	if len(r.Parts) == 0 {
		r.Parts = r.CanonicalContent()
		return
	}
	r.Content = textForKinds(r.Parts, ContentText, ContentCommentary, ContentFinalAnswer)
}

func textForKinds(parts []ContentPart, kinds ...ContentKind) string {
	var output bytes.Buffer
	for _, part := range parts {
		for _, kind := range kinds {
			if part.Kind == kind {
				output.WriteString(part.Text)
				break
			}
		}
	}
	return output.String()
}
