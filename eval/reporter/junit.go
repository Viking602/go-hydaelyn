package reporter

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/Viking602/venat/eval"
)

// JUnit renders results as a JUnit XML document for CI ingestion. The output
// conforms to the widely-used JUnit schema: a single <testsuites> root holds
// one <testsuite>, and each EvalResult becomes one <testcase>. A failing case
// emits a <failure> child per failed assertion's detail.
type JUnit struct {
	// SuiteName names the emitted <testsuite>. Defaults to "eval" when empty.
	SuiteName string
}

// junitTestsuites mirrors the JUnit <testsuites> root element.
type junitTestsuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestsuite `xml:"testsuite"`
}

// junitTestsuite mirrors a JUnit <testsuite> element.
type junitTestsuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      string          `xml:"time,attr"`
	TestCases []junitTestCase `xml:"testcase"`
}

// junitTestCase mirrors a JUnit <testcase> element.
type junitTestCase struct {
	Name      string         `xml:"name,attr"`
	ClassName string         `xml:"classname,attr"`
	Time      string         `xml:"time,attr"`
	Failures  []junitFailure `xml:"failure,omitempty"`
}

// junitFailure mirrors a JUnit <failure> element. The detail text is the
// element's character data; Message is the short summary attribute.
type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Detail  string `xml:",chardata"`
}

// Render returns the JUnit XML document as bytes, prefixed with the XML
// declaration. The bytes round-trip through encoding/xml and validate against
// the JUnit schema (see junit_test.go).
func (r JUnit) Render(results []eval.EvalResult) ([]byte, error) {
	doc := r.build(results)
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal junit xml: %w", err)
	}
	out := append([]byte(xml.Header), body...)
	out = append(out, '\n')
	return out, nil
}

// build folds results into the JUnit document tree.
func (r JUnit) build(results []eval.EvalResult) junitTestsuites {
	name := r.SuiteName
	if name == "" {
		name = "eval"
	}

	var totalFailures int
	var totalTime float64
	cases := make([]junitTestCase, 0, len(results))
	for _, res := range results {
		tc := junitTestCase{
			Name:      res.Case,
			ClassName: name,
			Time:      formatSeconds(res.Duration.Seconds()),
		}
		totalTime += res.Duration.Seconds()
		if !res.Passed {
			totalFailures++
			for _, f := range res.Failures {
				tc.Failures = append(tc.Failures, junitFailure{
					Message: f.Assertion,
					Type:    "AssertionFailure",
					Detail:  f.Detail,
				})
			}
		}
		cases = append(cases, tc)
	}

	suite := junitTestsuite{
		Name:      name,
		Tests:     len(results),
		Failures:  totalFailures,
		Errors:    0,
		Skipped:   0,
		Time:      formatSeconds(totalTime),
		TestCases: cases,
	}
	return junitTestsuites{
		Tests:    len(results),
		Failures: totalFailures,
		Time:     formatSeconds(totalTime),
		Suites:   []junitTestsuite{suite},
	}
}

// formatSeconds renders a duration in seconds with fixed precision so the
// emitted XML is byte-stable for deterministic cases and parses as the
// schema's xs:decimal time attribute.
func formatSeconds(seconds float64) string {
	return strings.TrimSpace(fmt.Sprintf("%.3f", seconds))
}
