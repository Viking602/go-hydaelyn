package aiops_test

import (
	"testing"

	"github.com/Viking602/venat/packs/aiops"
)

func TestPack_ToolsMatchCapabilities(t *testing.T) {
	names := map[string]struct{}{}
	for _, manifest := range aiops.Pack.Capabilities {
		for _, cap := range manifest.Capabilities {
			if cap.Name == "" {
				t.Fatalf("capability missing name: %+v", cap)
			}
			names[cap.Name] = struct{}{}
		}
	}
	for _, agent := range aiops.Pack.Agents {
		for _, tool := range agent.Tools {
			if _, ok := names[tool]; !ok {
				t.Errorf("agent %q tool %q is not declared on a capability", agent.ID, tool)
			}
		}
		for _, cap := range agent.Capabilities {
			if _, ok := names[cap]; !ok {
				t.Errorf("agent %q capability %q is not declared on a capability manifest", agent.ID, cap)
			}
		}
	}
	if aiops.Pack.Name != aiops.PackName {
		t.Fatalf("pack name %q != PackName %q", aiops.Pack.Name, aiops.PackName)
	}
}
