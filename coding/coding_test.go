package coding

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/policy"
	"github.com/Viking602/venat/tool"
)

func TestNewToolSet_RegistersAllTools(t *testing.T) {
	ws, _ := newTestWorkspace(t, nil)
	set := NewToolSet(ws)
	got := make(map[string]bool, len(set))
	for _, d := range set {
		got[d.Definition().Name] = true
	}
	for _, name := range []string{
		ToolListFiles, ToolReadFile, ToolSearch, ToolEditHashline,
		ToolWriteFile, ToolGofmt, ToolGoTest, ToolGitDiff,
	} {
		if !got[name] {
			t.Errorf("toolset missing %q", name)
		}
	}
}

func TestToolMetadata_MatchesSpecTable(t *testing.T) {
	ws, _ := newTestWorkspace(t, nil)
	set := NewToolSet(ws)
	byName := make(map[string]tool.Definition, len(set))
	for _, d := range set {
		byName[d.Definition().Name] = d.Definition()
	}

	type want struct {
		effect             tool.EffectType
		requiresActionTask bool
		risk               string
		tags               []string
	}
	wants := map[string]want{
		ToolListFiles:    {tool.EffectReadOnly, false, riskLow, []string{tagCoding, tagRead}},
		ToolReadFile:     {tool.EffectReadOnly, false, riskLow, []string{tagCoding, tagRead}},
		ToolSearch:       {tool.EffectReadOnly, false, riskLow, []string{tagCoding, tagSearch}},
		ToolGitDiff:      {tool.EffectReadOnly, false, riskLow, []string{tagCoding, tagGit, tagDiff}},
		ToolEditHashline: {tool.EffectWrite, true, riskMedium, []string{tagCoding, tagEdit, tagHashline, tagWorkspace}},
		ToolWriteFile:    {tool.EffectWrite, true, riskMedium, []string{tagCoding, tagCreate}},
		ToolGofmt:        {tool.EffectWrite, true, riskLow, []string{tagCoding, tagFormat}},
		ToolGoTest:       {tool.EffectReadOnly, false, riskMedium, []string{tagCoding, tagTest, tagRun}},
	}
	for name, w := range wants {
		def := byName[name]
		if def.EffectType != w.effect {
			t.Errorf("%s effect = %q, want %q", name, def.EffectType, w.effect)
		}
		if def.RequiresActionTask != w.requiresActionTask {
			t.Errorf("%s requiresActionTask = %v, want %v", name, def.RequiresActionTask, w.requiresActionTask)
		}
		if def.RiskLevel != w.risk {
			t.Errorf("%s risk = %q, want %q", name, def.RiskLevel, w.risk)
		}
		if !equalStrings(def.PolicyTags, w.tags) {
			t.Errorf("%s policyTags = %v, want %v", name, def.PolicyTags, w.tags)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPolicyEngine_ReadAllowedWriteDenied(t *testing.T) {
	eng := PolicyEngine()
	ctx := context.Background()

	readTool := &api.Tool{
		Name:       ToolReadFile,
		EffectType: api.ToolEffectReadOnly,
		PolicyTags: []string{tagCoding, tagRead},
	}
	dec, err := eng.Authorize(ctx, policy.Request{Operation: policy.OperationToolCall, Tool: readTool})
	if err != nil {
		t.Fatalf("authorize read: %v", err)
	}
	if dec.Effect != policy.EffectAllow {
		t.Errorf("read tool effect = %q, want allow", dec.Effect)
	}

	editTool := &api.Tool{
		Name:               ToolEditHashline,
		EffectType:         api.ToolEffectWrite,
		RequiresActionTask: true,
		PolicyTags:         []string{tagCoding, tagEdit, tagHashline, tagWorkspace},
	}
	dec, err = eng.Authorize(ctx, policy.Request{Operation: policy.OperationToolCall, Tool: editTool})
	if err != nil {
		t.Fatalf("authorize edit: %v", err)
	}
	if dec.Effect != policy.EffectDeny {
		t.Errorf("edit tool effect = %q, want deny (default deny side-effects)", dec.Effect)
	}
}

func TestPolicyEngine_DeleteRunTagRequiresApproval(t *testing.T) {
	eng := PolicyEngine()
	ctx := context.Background()
	for _, tag := range []string{tagDelete, tagRun} {
		toolDef := &api.Tool{
			Name:       "coding.custom",
			EffectType: api.ToolEffectWrite,
			PolicyTags: []string{tagCoding, tag},
		}
		dec, err := eng.Authorize(ctx, policy.Request{Operation: policy.OperationToolCall, Tool: toolDef})
		if err != nil {
			t.Fatalf("authorize %s: %v", tag, err)
		}
		if dec.Effect != policy.EffectRequireApproval {
			t.Errorf("tag %q effect = %q, want require_approval", tag, dec.Effect)
		}
	}
}

func TestPolicyEngine_GoTestRequiresApproval(t *testing.T) {
	// go_test compiles and executes workspace code, so it carries the run tag
	// and must be escalated to require approval by the coding policy engine —
	// it must never run unattended just because it is classified read-only.
	ws, _ := newTestWorkspace(t, nil)
	set := NewToolSet(ws)
	def := driverDefByName(set, ToolGoTest)

	eng := PolicyEngine()
	dec, err := eng.Authorize(context.Background(), policy.Request{
		Operation: policy.OperationToolCall,
		Tool: &api.Tool{
			Name:       def.Name,
			EffectType: api.ToolEffectReadOnly,
			PolicyTags: def.PolicyTags,
		},
	})
	if err != nil {
		t.Fatalf("authorize go_test: %v", err)
	}
	if dec.Effect != policy.EffectRequireApproval {
		t.Errorf("go_test effect = %q, want require_approval", dec.Effect)
	}
}

// driverDefByName returns the tool.Definition for the named driver in set.
func driverDefByName(set []tool.Driver, name string) tool.Definition {
	for _, d := range set {
		if def := d.Definition(); def.Name == name {
			return def
		}
	}
	return tool.Definition{}
}

func TestAgentClass_ShapeAndTools(t *testing.T) {
	cls := AgentClass()
	if cls.Name != AgentClassName {
		t.Errorf("class name = %q, want %q", cls.Name, AgentClassName)
	}
	if cls.Instructions != Instructions {
		t.Error("class instructions should be the package Instructions constant")
	}
	// The class advertises the navigation/edit toolset but NOT write_file
	// (existing files are edited via edit_hashline; new files are created by
	// the host, not the agent loop's default tool list).
	wantTools := map[string]bool{
		ToolListFiles: true, ToolReadFile: true, ToolSearch: true,
		ToolEditHashline: true, ToolGofmt: true, ToolGoTest: true, ToolGitDiff: true,
	}
	if len(cls.Tools) != len(wantTools) {
		t.Errorf("class tools = %v", cls.Tools)
	}
	for _, name := range cls.Tools {
		if !wantTools[name] {
			t.Errorf("unexpected tool in class: %q", name)
		}
	}
}
