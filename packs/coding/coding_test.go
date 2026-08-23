package coding_test

import (
	"testing"

	runcoding "github.com/Viking602/venat/coding"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/packs/coding"
)

func TestCodingPack_SmokeSuite(t *testing.T) {
	results := eval.RunSuite(t, coding.SmokeCases)
	if len(results) != len(coding.SmokeCases) {
		t.Fatalf("RunSuite returned %d results, want %d", len(results), len(coding.SmokeCases))
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("eval case %q failed: %+v", res.Case, res.Failures)
		}
	}
}

func TestCodingPack_Shape(t *testing.T) {
	if coding.Pack.Name != coding.PackName || coding.Pack.Version == "" {
		t.Fatalf("pack name/version: %q / %q", coding.Pack.Name, coding.Pack.Version)
	}
	if len(coding.Pack.Agents) != 1 {
		t.Fatalf("want exactly one agent, got %d", len(coding.Pack.Agents))
	}
	if len(coding.Pack.Capabilities) != 1 {
		t.Fatalf("want exactly one capability manifest, got %d", len(coding.Pack.Capabilities))
	}
	caps := coding.Pack.Capabilities[0].Capabilities
	if got, want := len(caps), 8; got != want {
		t.Fatalf("want %d coding capabilities, got %d", want, got)
	}
	for _, c := range caps {
		if c.Name == "" || c.EffectType == "" {
			t.Errorf("capability missing name/effect: %+v", c)
		}
	}
}

func TestCodingPack_ToolNamesMatchRuntime(t *testing.T) {
	want := map[string]struct{}{
		runcoding.ToolListFiles:    {},
		runcoding.ToolReadFile:     {},
		runcoding.ToolSearch:       {},
		runcoding.ToolGitDiff:      {},
		runcoding.ToolEditHashline: {},
		runcoding.ToolWriteFile:    {},
		runcoding.ToolGofmt:        {},
		runcoding.ToolGoTest:       {},
	}
	got := map[string]struct{}{}
	for _, cap := range coding.Pack.Capabilities[0].Capabilities {
		got[cap.Name] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("capability names = %d, runtime tools = %d", len(got), len(want))
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("runtime tool %q missing from pack capabilities", name)
		}
	}
	for _, tool := range coding.Pack.Agents[0].Tools {
		if _, ok := want[tool]; !ok {
			t.Errorf("agent tool %q is not a runtime coding tool", tool)
		}
	}
}
