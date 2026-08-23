package message

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMessageContentRoundTripPreservesOrderAndModalities(t *testing.T) {
	input := Message{
		Role: RoleAssistant,
		Content: []ContentPart{
			ReasoningPart("inspect", "sig-1"),
			CommentaryPart("checking"),
			{Kind: ContentImage, ID: "image-1", Data: []byte{1, 2, 3}, MediaType: "image/png", URI: "artifact://image-1"},
			{Kind: ContentSource, Source: &Source{ID: "source-1", URL: "https://example.com", Title: "Example"}},
			FinalAnswerPart("done"),
		},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output Message
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output.Content, input.Content) {
		t.Fatalf("content round trip = %#v, want %#v", output.Content, input.Content)
	}
	if output.TextContent() != "checkingdone" || output.FinalAnswerContent() != "done" || output.ReasoningContent() != "inspect" {
		t.Fatalf("projected text: visible=%q final=%q reasoning=%q", output.TextContent(), output.FinalAnswerContent(), output.ReasoningContent())
	}
}

func TestCanonicalContentPromotesLegacyMessageFields(t *testing.T) {
	message := Message{Text: "answer", Thinking: "reason", ThinkingSignature: "sig"}
	message.SyncLegacyContent()
	if len(message.Content) != 2 || message.Content[0].Kind != ContentReasoning || message.Content[0].Signature != "sig" || message.Content[1].Kind != ContentText {
		t.Fatalf("promoted content = %#v", message.Content)
	}
	message.Content[0].Text = "updated"
	message.Content[1].Text = "final"
	message.SyncLegacyContent()
	if message.Thinking != "updated" || message.Text != "final" {
		t.Fatalf("legacy projection: thinking=%q text=%q", message.Thinking, message.Text)
	}
}

func TestCloneContentDoesNotAliasBinaryOrSourceData(t *testing.T) {
	parts := []ContentPart{{Kind: ContentImage, Data: []byte{1}, ProviderData: json.RawMessage(`{"opaque":true}`), Source: &Source{Title: "source"}}}
	cloned := CloneContent(parts)
	cloned[0].Data[0] = 2
	cloned[0].ProviderData[0] = '['
	cloned[0].Source.Title = "changed"
	if parts[0].Data[0] != 1 || string(parts[0].ProviderData) != `{"opaque":true}` || parts[0].Source.Title != "source" {
		t.Fatalf("clone mutated source = %#v", parts)
	}
}

func TestToolResultContentSupportsTextAndImageBlocks(t *testing.T) {
	result := ToolResult{Parts: []ContentPart{TextPart("caption"), {Kind: ContentImage, Data: []byte{1}, MediaType: "image/png"}}}
	result.SyncLegacyContent()
	if result.Content != "caption" || len(result.CanonicalContent()) != 2 {
		t.Fatalf("tool result content = %#v", result)
	}
}
