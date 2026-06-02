// Package matcher ships the framework's built-in value comparators used by
// eval assertions (most notably ToolCalledWithArg and OutputContains). A
// Matcher folds an arbitrary observed value into a pass/fail verdict plus a
// human-readable detail string, so the same comparator vocabulary works
// against tool arguments, blackboard payloads, and run output alike.
//
// ADR-009 note: Matcher.Match takes actual as `any` because it is a genuine
// generic comparison over heterogeneous observed values — `any` as a
// parameter is allowed; every return is typed.
package matcher

import (
	"fmt"
	"regexp"
	"strings"
)

// Matcher folds an observed value into a pass/fail verdict. Match returns
// (true, "") when the value satisfies the matcher, or (false, detail) where
// detail explains the mismatch for inclusion in an AssertionFailure.
type Matcher interface {
	// Match reports whether actual satisfies the matcher. The detail string is
	// empty on a match and explains the mismatch otherwise.
	Match(actual any) (bool, string)
}

// containsSubstring matches when the observed value, rendered as text,
// contains a substring.
type containsSubstring struct {
	substring     string
	caseSensitive bool
}

// ContainsSubstring returns a Matcher that passes when the observed value,
// rendered as text via fmt.Sprint, contains s. Matching is case-sensitive.
func ContainsSubstring(s string) Matcher {
	return containsSubstring{substring: s, caseSensitive: true}
}

// ContainsSubstringFold returns a case-insensitive ContainsSubstring matcher.
func ContainsSubstringFold(s string) Matcher {
	return containsSubstring{substring: s, caseSensitive: false}
}

// Match reports whether the rendered value contains the substring.
func (m containsSubstring) Match(actual any) (bool, string) {
	hay := renderText(actual)
	needle := m.substring
	if !m.caseSensitive {
		hay = strings.ToLower(hay)
		needle = strings.ToLower(needle)
	}
	if strings.Contains(hay, needle) {
		return true, ""
	}
	return false, fmt.Sprintf("value %q does not contain %q", renderText(actual), m.substring)
}

// regexMatch matches when the observed value, rendered as text, matches a
// compiled regular expression.
type regexMatch struct {
	pattern string
	re      *regexp.Regexp
	err     error
}

// RegexMatch returns a Matcher that passes when the observed value, rendered
// as text via fmt.Sprint, matches pattern. An invalid pattern is reported as a
// mismatch from Match rather than panicking, so a malformed assertion fails
// the case instead of the test binary.
func RegexMatch(pattern string) Matcher {
	re, err := regexp.Compile(pattern)
	return regexMatch{pattern: pattern, re: re, err: err}
}

// Match reports whether the rendered value matches the pattern.
func (m regexMatch) Match(actual any) (bool, string) {
	if m.err != nil {
		return false, fmt.Sprintf("invalid regex %q: %v", m.pattern, m.err)
	}
	text := renderText(actual)
	if m.re.MatchString(text) {
		return true, ""
	}
	return false, fmt.Sprintf("value %q does not match pattern %q", text, m.pattern)
}

// renderText renders an observed value as the text a string matcher inspects.
// A string is used verbatim; a []byte is interpreted as UTF-8; every other
// value is rendered with fmt.Sprint so matchers can run against structured
// tool arguments without the caller pre-stringifying them.
func renderText(actual any) string {
	switch v := actual.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(actual)
	}
}
