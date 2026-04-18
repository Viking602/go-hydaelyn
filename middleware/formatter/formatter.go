// Package formatter validates assistant output against an OutputSpec and
// produces a structured retry message when violations are found. It is a
// pure, dependency-light checker: callers decide when to invoke it and how
// to feed the retry message back into the conversation loop.
package formatter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Viking602/go-hydaelyn/message"
)

// OutputSpec declares the structural contract an assistant message must
// follow. Every field is optional; leaving one at its zero value disables
// that particular check.
type OutputSpec struct {
	// FirstParagraphSingleSentence requires the first paragraph (the text
	// before the first blank line or header) to contain exactly one
	// sentence terminated by a single punctuation mark.
	FirstParagraphSingleSentence bool
	// RequireHeaderBlankLines asks for a blank line immediately before and
	// after every markdown header, except when the header is the first
	// non-empty line in the document.
	RequireHeaderBlankLines bool
	// ForbidEmptySections rejects any header whose body is blank before
	// the next header or end of input.
	ForbidEmptySections bool
	// RequiredSections lists header titles (without leading hashes) that
	// must appear somewhere in the output.
	RequiredSections []string
	// AllowedSections, when non-empty, restricts output to only these
	// header titles (without leading hashes).
	AllowedSections []string
}

// Violation is a single failure produced by Validate.
type Violation struct {
	Code    string
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("[%s] %s", v.Code, v.Message)
}

// Validate runs every enabled check in spec against text and returns the
// violations it finds. An empty slice means the output passes.
func Validate(text string, spec OutputSpec) []Violation {
	var violations []Violation
	lines := strings.Split(text, "\n")

	if spec.FirstParagraphSingleSentence {
		if v := checkFirstParagraph(lines); v != nil {
			violations = append(violations, *v)
		}
	}
	if spec.RequireHeaderBlankLines {
		violations = append(violations, checkHeaderSpacing(lines)...)
	}

	headers := extractHeaders(lines)
	if spec.ForbidEmptySections {
		for _, h := range headers {
			if strings.TrimSpace(h.Body) == "" {
				violations = append(violations, Violation{
					Code:    "empty_section",
					Message: fmt.Sprintf("章节 %q 内容为空", h.Title),
				})
			}
		}
	}
	if len(spec.RequiredSections) > 0 {
		seen := make(map[string]bool, len(headers))
		for _, h := range headers {
			seen[h.Title] = true
		}
		for _, req := range spec.RequiredSections {
			if !seen[req] {
				violations = append(violations, Violation{
					Code:    "missing_required_section",
					Message: fmt.Sprintf("缺少必需章节: %s", req),
				})
			}
		}
	}
	if len(spec.AllowedSections) > 0 {
		allowed := make(map[string]bool, len(spec.AllowedSections))
		for _, s := range spec.AllowedSections {
			allowed[s] = true
		}
		for _, h := range headers {
			if !allowed[h.Title] {
				violations = append(violations, Violation{
					Code:    "disallowed_section",
					Message: fmt.Sprintf("未授权章节: %s", h.Title),
				})
			}
		}
	}
	return violations
}

// MetricSink is the minimal counter/histogram surface the formatter needs
// to report rumination and retry signals. It is intentionally structural
// so any observe.Observer satisfies it without an import cycle.
type MetricSink interface {
	IncCounter(name string, delta int64, attrs map[string]string)
	ObserveHistogram(name string, value float64, attrs map[string]string)
}

// Score summarises how much a block of text shows signs of self-correction
// loops ("Wait, actually, let me re-check..."). It is a heuristic signal
// suitable for observability dashboards, not a hard guard.
type Score struct {
	// Hits is the number of rumination markers found.
	Hits int
	// Tokens is a coarse word/char count used as the ratio denominator.
	Tokens int
	// Ratio is Hits divided by Tokens; 0 when Tokens == 0.
	Ratio float64
	// Markers lists the individual phrases matched, in order of appearance.
	Markers []string
}

var ruminationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bwait,?`),
	regexp.MustCompile(`(?i)\bactually,?`),
	regexp.MustCompile(`(?i)\blet me (?:re-?check|reconsider|think again|reconsider)`),
	regexp.MustCompile(`(?i)\bon second thought\b`),
	regexp.MustCompile(`(?i)\bhmm,?`),
	regexp.MustCompile(`让我再`),
	regexp.MustCompile(`重新(?:检查|考虑|想)`),
	regexp.MustCompile(`等一下`),
	regexp.MustCompile(`再想想`),
	regexp.MustCompile(`稍等`),
}

// Report emits the score as two metrics on sink under prefix: a hits
// counter (prefix+".rumination_hits") and a ratio histogram
// (prefix+".rumination_ratio"). No-op when sink is nil or prefix is empty.
func (s Score) Report(sink MetricSink, prefix string, attrs map[string]string) {
	if sink == nil || prefix == "" {
		return
	}
	sink.IncCounter(prefix+".rumination_hits", int64(s.Hits), attrs)
	sink.ObserveHistogram(prefix+".rumination_ratio", s.Ratio, attrs)
}

// RuminationScore scans text for reflection markers that usually indicate
// the model is reasoning in circles. A higher Ratio means a higher share
// of the output is meta-thinking rather than answer content.
func RuminationScore(text string) Score {
	if text == "" {
		return Score{}
	}
	var markers []string
	for _, pattern := range ruminationPatterns {
		markers = append(markers, pattern.FindAllString(text, -1)...)
	}
	tokens := len(strings.Fields(text))
	if tokens == 0 {
		// Fall back to rune count for CJK inputs with no whitespace.
		tokens = len([]rune(text))
	}
	score := Score{Hits: len(markers), Tokens: tokens, Markers: markers}
	if tokens > 0 {
		score.Ratio = float64(score.Hits) / float64(tokens)
	}
	return score
}

// Render turns an OutputSpec into a deterministic Simplified-Chinese prompt
// block. The block is meant to be injected as (or appended to) a system
// message so the model sees the rules as an unambiguous decision table
// rather than free-form prose. Returns an empty string when spec has no
// enabled rules, so callers can safely concat it unconditionally.
func Render(spec OutputSpec) string {
	if !hasAnyRule(spec) {
		return ""
	}
	var b strings.Builder
	b.WriteString("输出格式约束（按规则执行，冲突时按列表顺序裁决）：\n")
	idx := 1
	if spec.FirstParagraphSingleSentence {
		fmt.Fprintf(&b, "%d. 首段必须且仅有一句话，作为结论。\n", idx)
		idx++
	}
	if len(spec.RequiredSections) > 0 {
		fmt.Fprintf(&b, "%d. 必须包含以下章节（标题精确一致）：%s。\n",
			idx, strings.Join(spec.RequiredSections, "、"))
		idx++
	}
	if len(spec.AllowedSections) > 0 {
		fmt.Fprintf(&b, "%d. 只允许出现以下章节，其他一律不输出：%s。\n",
			idx, strings.Join(spec.AllowedSections, "、"))
		idx++
	}
	if spec.ForbidEmptySections {
		fmt.Fprintf(&b, "%d. 禁止输出空章节；没有对应数据时，连同标题一起整段省略。\n", idx)
		idx++
	}
	if spec.RequireHeaderBlankLines {
		fmt.Fprintf(&b, "%d. 每个标题前后各留一个空行；列表项各占一行。\n", idx)
		idx++
	}
	b.WriteString("\n输出前只做一轮自检，不要反复修改或表达犹豫。\n")
	return b.String()
}

func hasAnyRule(spec OutputSpec) bool {
	return spec.FirstParagraphSingleSentence ||
		spec.RequireHeaderBlankLines ||
		spec.ForbidEmptySections ||
		len(spec.RequiredSections) > 0 ||
		len(spec.AllowedSections) > 0
}

// BuildRetryMessage wraps violations in a user-role message that can be
// appended to the conversation to ask the model to fix its output without
// rewriting unrelated parts.
func BuildRetryMessage(violations []Violation) message.Message {
	var b strings.Builder
	b.WriteString("上一次输出不符合格式规范，请按以下问题修正后重新输出：\n\n")
	for _, v := range violations {
		b.WriteString("- ")
		b.WriteString(v.String())
		b.WriteString("\n")
	}
	b.WriteString("\n只输出修正后的正式内容，不要解释修改理由。")
	return message.NewText(message.RoleUser, b.String())
}

type header struct {
	Title string
	Body  string
}

func extractHeaders(lines []string) []header {
	var out []header
	var current *header
	var body strings.Builder
	flush := func() {
		if current == nil {
			return
		}
		current.Body = body.String()
		out = append(out, *current)
		current = nil
		body.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			flush()
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			current = &header{Title: title}
			continue
		}
		if current != nil {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return out
}

var sentenceTerminator = regexp.MustCompile(`[.。!！?？]`)

func checkFirstParagraph(lines []string) *Violation {
	var buf []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			break
		}
		buf = append(buf, trimmed)
	}
	if len(buf) == 0 {
		return nil
	}
	para := strings.Join(buf, " ")
	matches := sentenceTerminator.FindAllString(para, -1)
	count := len(matches)
	if count == 0 && strings.TrimSpace(para) != "" {
		count = 1
	}
	if count > 1 {
		return &Violation{
			Code:    "first_paragraph_multi_sentence",
			Message: "首段必须只有一句话",
		}
	}
	return nil
}

func checkHeaderSpacing(lines []string) []Violation {
	var out []Violation
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Skip the blank-before check when the header is the first
		// non-empty line in the document.
		firstNonEmpty := true
		for j := 0; j < i; j++ {
			if strings.TrimSpace(lines[j]) != "" {
				firstNonEmpty = false
				break
			}
		}
		if !firstNonEmpty && i > 0 {
			if strings.TrimSpace(lines[i-1]) != "" {
				out = append(out, Violation{
					Code:    "header_missing_blank_before",
					Message: fmt.Sprintf("第 %d 行标题前缺少空行: %s", i+1, trimmed),
				})
			}
		}
		if i+1 < len(lines) {
			if strings.TrimSpace(lines[i+1]) != "" {
				out = append(out, Violation{
					Code:    "header_missing_blank_after",
					Message: fmt.Sprintf("第 %d 行标题后缺少空行: %s", i+1, trimmed),
				})
			}
		}
	}
	return out
}
