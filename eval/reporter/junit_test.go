package reporter_test

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/reporter"
)

func sampleResults() []eval.EvalResult {
	return []eval.EvalResult{
		{
			Case:     "passing-case",
			Run:      api.Run{ID: "run-1", Status: api.RunStatusCompleted},
			Passed:   true,
			Duration: 12 * time.Millisecond,
		},
		{
			Case:   "failing-case",
			Run:    api.Run{ID: "run-2", Status: api.RunStatusFailed},
			Passed: false,
			Failures: []eval.AssertionFailure{
				{Assertion: "OutputContains", Detail: `value "ok" does not contain "boom"`},
				{Assertion: "RunTerminatedWithStatus", Detail: "status was failed, want completed"},
			},
			Duration: 7 * time.Millisecond,
		},
	}
}

func TestReporter_JUnit_OutputValidatesAgainstXSD(t *testing.T) {
	out, err := reporter.JUnit{SuiteName: "demo"}.Render(sampleResults())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	schema := loadXSD(t, "testdata/junit.xsd")
	root := parseXML(t, out)
	if err := schema.validate(root); err != nil {
		t.Fatalf("emitted JUnit XML does not validate against the JUnit XSD: %v\n%s", err, out)
	}
}

func TestReporter_JUnit_AggregatesTotals(t *testing.T) {
	out, err := reporter.JUnit{}.Render(sampleResults())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	var doc struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Suites   []struct {
			Name      string `xml:"name,attr"`
			Tests     int    `xml:"tests,attr"`
			Failures  int    `xml:"failures,attr"`
			TestCases []struct {
				Name     string `xml:"name,attr"`
				Failures []struct {
					Message string `xml:"message,attr"`
					Detail  string `xml:",chardata"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal emitted xml: %v", err)
	}

	if doc.Tests != 2 {
		t.Errorf("root tests = %d, want 2", doc.Tests)
	}
	if doc.Failures != 1 {
		t.Errorf("root failures = %d, want 1", doc.Failures)
	}
	if len(doc.Suites) != 1 {
		t.Fatalf("got %d suites, want 1", len(doc.Suites))
	}
	suite := doc.Suites[0]
	if suite.Name != "eval" {
		t.Errorf("suite name = %q, want default %q", suite.Name, "eval")
	}
	if len(suite.TestCases) != 2 {
		t.Fatalf("got %d testcases, want 2", len(suite.TestCases))
	}
	if got := len(suite.TestCases[1].Failures); got != 2 {
		t.Fatalf("failing case got %d failure elements, want 2", got)
	}
	if suite.TestCases[1].Failures[0].Message != "OutputContains" {
		t.Errorf("failure message = %q, want %q", suite.TestCases[1].Failures[0].Message, "OutputContains")
	}
}

func TestReporter_JUnit_EmptySuiteIsValid(t *testing.T) {
	out, err := reporter.JUnit{}.Render(nil)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	schema := loadXSD(t, "testdata/junit.xsd")
	root := parseXML(t, out)
	if err := schema.validate(root); err != nil {
		t.Fatalf("empty suite did not validate: %v\n%s", err, out)
	}
}

// --- minimal structural XSD validator (pure Go) -----------------------------
//
// The repo ships no CGO libxml2 binding and the contribution guide discourages
// new dependency stacks, so the test validates the emitted document against the
// JUnit XSD by parsing the schema's element/attribute declarations and checking
// the document's parse tree honors them: element names, allowed children,
// required/optional attributes, and xs:int / xs:decimal simpleTypes.

// xsdElement is one <xs:element> declaration resolved from the schema.
type xsdElement struct {
	name       string
	attrs      map[string]xsdAttr
	childOrder []string        // names allowed as children (by ref)
	children   map[string]bool // set form of childOrder for fast lookup
}

type xsdAttr struct {
	name     string
	typ      string // xs:int, xs:decimal, xs:string
	required bool
}

type xsdSchema struct {
	elements map[string]xsdElement
}

func loadXSD(t *testing.T, path string) xsdSchema {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read xsd: %v", err)
	}

	type xsdAttrXML struct {
		Name string `xml:"name,attr"`
		Type string `xml:"type,attr"`
		Use  string `xml:"use,attr"`
	}
	type xsdRefXML struct {
		Ref string `xml:"ref,attr"`
	}
	type xsdElementXML struct {
		Name        string `xml:"name,attr"`
		ComplexType struct {
			Sequence struct {
				Elements []xsdRefXML `xml:"element"`
			} `xml:"sequence"`
			Attributes    []xsdAttrXML `xml:"attribute"`
			SimpleContent struct {
				Extension struct {
					Base       string       `xml:"base,attr"`
					Attributes []xsdAttrXML `xml:"attribute"`
				} `xml:"extension"`
			} `xml:"simpleContent"`
		} `xml:"complexType"`
	}
	var schemaXML struct {
		Elements []xsdElementXML `xml:"element"`
	}
	if err := xml.Unmarshal(raw, &schemaXML); err != nil {
		t.Fatalf("parse xsd: %v", err)
	}

	schema := xsdSchema{elements: map[string]xsdElement{}}
	for _, e := range schemaXML.Elements {
		el := xsdElement{
			name:     e.Name,
			attrs:    map[string]xsdAttr{},
			children: map[string]bool{},
		}
		attrs := e.ComplexType.Attributes
		attrs = append(attrs, e.ComplexType.SimpleContent.Extension.Attributes...)
		for _, a := range attrs {
			el.attrs[a.Name] = xsdAttr{name: a.Name, typ: a.Type, required: a.Use == "required"}
		}
		for _, c := range e.ComplexType.Sequence.Elements {
			el.childOrder = append(el.childOrder, c.Ref)
			el.children[c.Ref] = true
		}
		schema.elements[e.Name] = el
	}
	return schema
}

// xmlNode is a parsed element of the document under test.
type xmlNode struct {
	name     string
	attrs    map[string]string
	children []*xmlNode
}

func parseXML(t *testing.T, doc []byte) *xmlNode {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(doc)))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch se := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: se.Name.Local, attrs: map[string]string{}}
			for _, a := range se.Attr {
				node.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else {
				root = node
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil {
		t.Fatalf("document has no root element")
	}
	return root
}

func (s xsdSchema) validate(node *xmlNode) error {
	decl, ok := s.elements[node.name]
	if !ok {
		return fmt.Errorf("element <%s> is not declared in the schema", node.name)
	}
	// Attributes: every present attribute must be declared and well-typed.
	for name, value := range node.attrs {
		attr, ok := decl.attrs[name]
		if !ok {
			return fmt.Errorf("<%s> has undeclared attribute %q", node.name, name)
		}
		if err := checkAttrType(attr, value); err != nil {
			return fmt.Errorf("<%s>@%s: %w", node.name, name, err)
		}
	}
	// Required attributes must be present.
	for name, attr := range decl.attrs {
		if attr.required {
			if _, present := node.attrs[name]; !present {
				return fmt.Errorf("<%s> is missing required attribute %q", node.name, name)
			}
		}
	}
	// Children must be declared, and recurse.
	for _, child := range node.children {
		if !decl.children[child.name] {
			return fmt.Errorf("<%s> may not contain child <%s>", node.name, child.name)
		}
		if err := s.validate(child); err != nil {
			return err
		}
	}
	return nil
}

func checkAttrType(attr xsdAttr, value string) error {
	switch attr.typ {
	case "xs:int":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("value %q is not a valid xs:int", value)
		}
	case "xs:decimal":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("value %q is not a valid xs:decimal", value)
		}
	case "xs:string", "":
		// Any string is valid.
	default:
		return fmt.Errorf("unknown simpleType %q", attr.typ)
	}
	return nil
}
