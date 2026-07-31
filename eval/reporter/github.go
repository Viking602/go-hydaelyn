package reporter

import (
	"fmt"
	"io"
	"strings"

	"github.com/Viking602/venat/eval"
)

// GitHub renders results as GitHub Actions workflow commands. Each failed
// assertion becomes an `::error::` annotation line, which GitHub surfaces as a
// Checks annotation on the run. The annotation's title property carries the
// suite title and case name (both escaped as property values); the failing
// assertion and its detail go in the message body, so no unescaped delimiter
// sits in the property region. A trailing `::notice::` line reports the suite
// totals. When every case passes, only the notice line is emitted.
//
// The format follows GitHub's workflow-command syntax:
// https://docs.github.com/actions/using-workflows/workflow-commands-for-github-actions
type GitHub struct {
	// Title prefixes each annotation's title property. Defaults to "eval".
	Title string
}

// Render returns the annotation lines as a single string.
func (r GitHub) Render(results []eval.EvalResult) string {
	var b strings.Builder
	r.write(&b, results)
	return b.String()
}

// Write renders the annotation lines to w. It returns the number of bytes
// written and any write error.
func (r GitHub) Write(w io.Writer, results []eval.EvalResult) (int, error) {
	return io.WriteString(w, r.Render(results))
}

func (r GitHub) write(b *strings.Builder, results []eval.EvalResult) {
	title := r.Title
	if title == "" {
		title = "eval"
	}

	var passed, failed int
	for _, res := range results {
		if res.Passed {
			passed++
			continue
		}
		failed++
		for _, f := range res.Failures {
			fmt.Fprintf(b, "::error title=%s::%s\n",
				escapeProperty(title+" "+res.Case),
				escapeData(f.Assertion+": "+f.Detail))
		}
	}
	fmt.Fprintf(b, "::notice title=%s::%d passed, %d failed, %d total\n",
		escapeProperty(title), passed, failed, passed+failed)
}

// escapeData escapes a workflow-command message per GitHub's rules: the message
// is the part after the final "::", so the percent sign and the carriage
// return / newline that would terminate the command must be encoded.
func escapeData(s string) string {
	r := strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	)
	return r.Replace(s)
}

// escapeProperty escapes a workflow-command property value (title=...). In
// addition to the message escapes, ":" and "," delimit properties and so are
// encoded too.
func escapeProperty(s string) string {
	r := strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	)
	return r.Replace(s)
}
