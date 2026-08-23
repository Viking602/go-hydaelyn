package venat_test

import (
	"testing"

	runcoding "github.com/Viking602/venat/coding"
	codingpack "github.com/Viking602/venat/packs/coding"
)

func TestCodingPackToolNamesMatchRuntime(t *testing.T) {
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
	got := make(map[string]struct{}, len(codingpack.Pack.Capabilities[0].Capabilities))
	for _, capability := range codingpack.Pack.Capabilities[0].Capabilities {
		got[capability.Name] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("capability names = %d, runtime tools = %d", len(got), len(want))
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("runtime tool %q missing from pack capabilities", name)
		}
	}
	for _, tool := range codingpack.Pack.Agents[0].Tools {
		if _, ok := want[tool]; !ok {
			t.Errorf("agent tool %q is not a runtime coding tool", tool)
		}
	}
}
