package formatter

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
)

func TestValidateNoSpecNoViolations(t *testing.T) {
	if got := Validate("anything\n\n## 无规则\n\n内容\n", OutputSpec{}); len(got) != 0 {
		t.Fatalf("expected no violations with empty spec, got %#v", got)
	}
}

func TestValidateFirstParagraphSingleSentence(t *testing.T) {
	spec := OutputSpec{FirstParagraphSingleSentence: true}
	okCases := []string{
		"这是唯一的一句话。\n",
		"A single sentence without punctuation\n",
		"前半段，后半段，只有一个句号。\n",
	}
	for _, text := range okCases {
		if got := Validate(text, spec); len(got) != 0 {
			t.Fatalf("expected %q to pass, got %#v", text, got)
		}
	}
	badCases := []string{
		"第一句。第二句。\n",
		"First. Second.\n",
	}
	for _, text := range badCases {
		violations := Validate(text, spec)
		if len(violations) == 0 || violations[0].Code != "first_paragraph_multi_sentence" {
			t.Fatalf("expected multi-sentence violation for %q, got %#v", text, violations)
		}
	}
}

func TestValidateForbidsEmptySections(t *testing.T) {
	spec := OutputSpec{ForbidEmptySections: true}
	text := "## 有内容\n\n正文\n\n## 空章节\n"
	violations := Validate(text, spec)
	if len(violations) != 1 || violations[0].Code != "empty_section" {
		t.Fatalf("expected one empty_section violation, got %#v", violations)
	}
	if !strings.Contains(violations[0].Message, "空章节") {
		t.Fatalf("violation message should reference the title, got %q", violations[0].Message)
	}
}

func TestValidateRequiresHeaderBlankLines(t *testing.T) {
	spec := OutputSpec{RequireHeaderBlankLines: true}
	text := "前言\n## 紧贴上文\n紧贴下文\n"
	violations := Validate(text, spec)
	var before, after bool
	for _, v := range violations {
		switch v.Code {
		case "header_missing_blank_before":
			before = true
		case "header_missing_blank_after":
			after = true
		}
	}
	if !before || !after {
		t.Fatalf("expected both before and after violations, got %#v", violations)
	}
}

func TestValidateRequiredAndAllowedSections(t *testing.T) {
	spec := OutputSpec{
		RequiredSections: []string{"结论", "要点"},
		AllowedSections:  []string{"结论", "要点"},
	}
	text := "## 结论\n\ntext\n\n## 闲聊\n\ntext\n"
	violations := Validate(text, spec)
	var missing, disallowed bool
	for _, v := range violations {
		switch v.Code {
		case "missing_required_section":
			if strings.Contains(v.Message, "要点") {
				missing = true
			}
		case "disallowed_section":
			if strings.Contains(v.Message, "闲聊") {
				disallowed = true
			}
		}
	}
	if !missing || !disallowed {
		t.Fatalf("expected missing + disallowed violations, got %#v", violations)
	}
}

func TestRuminationScoreEmptyInput(t *testing.T) {
	score := RuminationScore("")
	if score.Hits != 0 || score.Ratio != 0 || score.Tokens != 0 {
		t.Fatalf("expected zero score for empty input, got %#v", score)
	}
}

func TestRuminationScoreDetectsMarkers(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"english wait", "Wait, actually let me reconsider the plan.", 3},
		// "让我再想想" contains both 让我再 and 再想想, plus 重新检查 in the tail — 3 markers.
		{"chinese let me again", "让我再想想，其实这里需要重新检查一下。", 3},
		{"no markers", "Here is the clean final answer.", 0},
		{"case insensitive", "WAIT, Hmm, maybe Actually that's fine.", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RuminationScore(tc.text)
			if got.Hits != tc.want {
				t.Fatalf("Hits = %d, want %d (markers=%v)", got.Hits, tc.want, got.Markers)
			}
			if tc.want > 0 && got.Ratio <= 0 {
				t.Fatalf("expected positive ratio, got %f", got.Ratio)
			}
			if tc.want == 0 && got.Ratio != 0 {
				t.Fatalf("expected zero ratio for clean text, got %f", got.Ratio)
			}
		})
	}
}

func TestRuminationScoreCJKTokenFallback(t *testing.T) {
	// Pure CJK has no whitespace — the scorer should fall back to rune count
	// so ratio stays meaningful.
	score := RuminationScore("让我再想想")
	if score.Tokens == 0 {
		t.Fatalf("expected non-zero token count via rune fallback, got %#v", score)
	}
	if score.Ratio == 0 {
		t.Fatalf("expected non-zero ratio for CJK marker, got %#v", score)
	}
}

func TestRenderEmptySpecReturnsEmpty(t *testing.T) {
	if got := Render(OutputSpec{}); got != "" {
		t.Fatalf("expected empty render for empty spec, got %q", got)
	}
}

func TestRenderIncludesEnabledRules(t *testing.T) {
	spec := OutputSpec{
		FirstParagraphSingleSentence: true,
		ForbidEmptySections:          true,
		RequireHeaderBlankLines:      true,
		RequiredSections:             []string{"结论", "要点"},
		AllowedSections:              []string{"结论", "要点", "适用范围"},
	}
	text := Render(spec)
	expectations := []string{
		"首段必须且仅有一句话",
		"必须包含以下章节",
		"结论、要点",
		"只允许出现以下章节",
		"适用范围",
		"禁止输出空章节",
		"每个标题前后各留一个空行",
		"不要反复修改或表达犹豫",
	}
	for _, want := range expectations {
		if !strings.Contains(text, want) {
			t.Fatalf("render missing %q:\n%s", want, text)
		}
	}
}

func TestBuildRetryMessageContainsViolations(t *testing.T) {
	violations := []Violation{
		{Code: "empty_section", Message: "章节 \"警告\" 内容为空"},
		{Code: "missing_required_section", Message: "缺少必需章节: 结论"},
	}
	msg := BuildRetryMessage(violations)
	if msg.Role != message.RoleUser {
		t.Fatalf("expected user role, got %s", msg.Role)
	}
	for _, v := range violations {
		if !strings.Contains(msg.Text, v.Message) {
			t.Fatalf("retry message missing %q, got %q", v.Message, msg.Text)
		}
	}
}
