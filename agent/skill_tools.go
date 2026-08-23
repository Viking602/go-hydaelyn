package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/skill"
	"github.com/Viking602/venat/tool"
)

const (
	// These legacy Hydaelyn wire identifiers are intentionally stable across
	// the Venat project rename because transcripts and tool allowlists may
	// persist them outside this module.
	// SkillActivationToolName is the framework-owned read-only tool used for
	// model-driven skill activation.
	SkillActivationToolName = "hydaelyn_activate_skill"
	// SkillResourceToolName is the framework-owned read-only tool used to read
	// a declared resource from an active skill.
	SkillResourceToolName = "hydaelyn_read_skill_resource"

	activateSkillToolName     = SkillActivationToolName
	readSkillResourceToolName = SkillResourceToolName
	// skillContextMetadataKey is also a legacy persisted transcript key.
	skillContextMetadataKey = "hydaelyn.skill.context"
)

var errSkillToolConflict = errors.New("agent: reserved skill tool name is already registered")

type skillRuntime struct {
	mu        sync.RWMutex
	all       map[string]skill.Skill
	available map[string]skill.Skill
	active    map[string]struct{}
	eager     map[string]struct{}
	order     []string
}

func newSkillRuntime(active, available []skill.Skill) *skillRuntime {
	r := &skillRuntime{
		all:       make(map[string]skill.Skill, len(active)+len(available)),
		available: make(map[string]skill.Skill, len(available)),
		active:    make(map[string]struct{}, len(active)),
		eager:     make(map[string]struct{}, len(active)),
	}
	for _, current := range active {
		r.all[current.Name] = current
		r.active[current.Name] = struct{}{}
		r.eager[current.Name] = struct{}{}
		r.order = append(r.order, current.Name)
	}
	availableNames := make([]string, 0, len(available))
	for _, current := range available {
		if _, alreadyKnown := r.all[current.Name]; alreadyKnown {
			continue
		}
		r.all[current.Name] = current
		r.available[current.Name] = current
		availableNames = append(availableNames, current.Name)
	}
	sort.Strings(availableNames)
	r.order = append(r.order, availableNames...)
	return r
}

func (r *skillRuntime) attachTools(bus *tool.Bus) (*tool.Bus, error) {
	wantActivate := len(r.all) > 0
	wantRead := r.hasResources()
	if !wantActivate && !wantRead {
		return bus, nil
	}
	if bus != nil {
		if err := bus.Validate(); err != nil {
			return nil, err
		}
	}
	cloned := cloneToolBus(bus)
	for _, name := range []string{activateSkillToolName, readSkillResourceToolName} {
		if _, exists := cloned.Driver(name); exists {
			return nil, fmt.Errorf("%w: %s", errSkillToolConflict, name)
		}
	}
	if wantActivate {
		if err := cloned.Register(skillActivationDriver{runtime: r}); err != nil {
			return nil, err
		}
	}
	if wantRead {
		if err := cloned.Register(skillResourceDriver{runtime: r}); err != nil {
			return nil, err
		}
	}
	return cloned, nil
}

func cloneToolBus(bus *tool.Bus) *tool.Bus {
	return bus.Clone()
}

func (r *skillRuntime) hasResources() bool {
	for _, current := range r.all {
		if len(current.Resources) > 0 {
			return true
		}
	}
	return false
}

func (r *skillRuntime) activate(name string) (skill.Skill, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.all[name]
	if !ok {
		return skill.Skill{}, false, &skill.NotFoundError{Name: name}
	}
	if _, exists := r.active[name]; exists {
		return current, false, nil
	}
	r.active[name] = struct{}{}
	return current, true, nil
}

func (r *skillRuntime) activeSkill(name string) (skill.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, active := r.active[name]; !active {
		return skill.Skill{}, false
	}
	current, ok := r.all[name]
	return current, ok
}

func (r *skillRuntime) activeSkills() []skill.Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]skill.Skill, 0, len(r.order))
	for _, name := range r.order {
		if _, active := r.active[name]; !active {
			continue
		}
		if current, ok := r.all[name]; ok {
			out = append(out, current)
		}
	}
	return out
}

func (r *skillRuntime) restoreActivations(messages []message.Message) {
	present := activationResults(messages)
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, content := range present {
		if current, known := r.all[name]; known && content == skill.Activate(current) {
			r.active[name] = struct{}{}
		}
	}
}

func (r *skillRuntime) skillsForCompaction(messages []message.Message) []skill.Skill {
	present := activationResults(messages)
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]skill.Skill, 0, len(r.order))
	for _, name := range r.order {
		current, ok := r.all[name]
		if _, active := r.active[name]; active && ok && shouldRestoreSkill(name, current, present, r.eager) {
			out = append(out, current)
		}
	}
	return out
}

func activationResults(messages []message.Message) map[string]string {
	present := make(map[string]string)
	for _, current := range messages {
		if current.ToolResult == nil || current.ToolResult.Name != activateSkillToolName || current.ToolResult.IsError {
			continue
		}
		var result struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(current.ToolResult.Structured, &result) == nil && result.Name != "" {
			present[result.Name] = current.ToolResult.Content
		}
	}
	return present
}

func shouldRestoreSkill(name string, current skill.Skill, present map[string]string, eager map[string]struct{}) bool {
	if _, alwaysRestore := eager[name]; alwaysRestore {
		return true
	}
	content, alreadyPresent := present[name]
	return !alreadyPresent || content != skill.Activate(current)
}

func (r *skillRuntime) availableSkills() []skill.Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.available))
	for name := range r.available {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]skill.Skill, 0, len(names))
	for _, name := range names {
		out = append(out, r.available[name])
	}
	return out
}

type skillActivationDriver struct{ runtime *skillRuntime }

func (d skillActivationDriver) Definition() tool.Definition {
	d.runtime.mu.RLock()
	names := make([]string, 0, len(d.runtime.all))
	for name := range d.runtime.all {
		names = append(names, name)
	}
	d.runtime.mu.RUnlock()
	sort.Strings(names)
	additional := false
	return tool.Definition{
		Name:        activateSkillToolName,
		Description: "Load one available Agent Skill before following its instructions.",
		InputSchema: message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"name": {Type: "string", Enum: names},
			},
			Required:             []string{"name"},
			AdditionalProperties: &additional,
		},
		EffectType:       tool.EffectReadOnly,
		Idempotent:       true,
		Concurrency:      tool.ConcurrencySequential,
		ConcurrencyGroup: "skills",
	}
}

func (d skillActivationDriver) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var arguments struct {
		Name string `json:"name"`
	}
	if err := decodeSkillToolArguments(call.Arguments, &arguments); err != nil {
		return skillToolErrorResult(call, err), nil
	}
	current, activated, err := d.runtime.activate(arguments.Name)
	if err != nil {
		return skillToolErrorResult(call, err), nil
	}
	if !activated {
		return tool.Result{ToolCallID: call.ID, Name: call.Name, Content: "Skill already active: " + current.Name}, nil
	}
	structured, _ := json.Marshal(struct {
		Name      string           `json:"name"`
		Resources []skill.Resource `json:"resources,omitempty"`
	}{Name: current.Name, Resources: current.Resources})
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    skill.Activate(current),
		Structured: structured,
	}, nil
}

type skillResourceDriver struct{ runtime *skillRuntime }

func (d skillResourceDriver) Definition() tool.Definition {
	d.runtime.mu.RLock()
	names := make([]string, 0, len(d.runtime.all))
	for name := range d.runtime.all {
		names = append(names, name)
	}
	d.runtime.mu.RUnlock()
	sort.Strings(names)
	additional := false
	return tool.Definition{
		Name:        readSkillResourceToolName,
		Description: "Read one declared resource from an active Agent Skill.",
		InputSchema: message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"skill": {Type: "string", Enum: names},
				"path":  {Type: "string"},
			},
			Required:             []string{"skill", "path"},
			AdditionalProperties: &additional,
		},
		EffectType:       tool.EffectReadOnly,
		Idempotent:       true,
		Concurrency:      tool.ConcurrencySequential,
		ConcurrencyGroup: "skills",
	}
}

func (d skillResourceDriver) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var arguments struct {
		Skill string `json:"skill"`
		Path  string `json:"path"`
	}
	if err := decodeSkillToolArguments(call.Arguments, &arguments); err != nil {
		return skillToolErrorResult(call, err), nil
	}
	current, active := d.runtime.activeSkill(arguments.Skill)
	if !active {
		return skillToolErrorResult(call, fmt.Errorf("agent: skill %q is not active", arguments.Skill)), nil
	}
	content, err := skill.ReadResource(current, arguments.Path)
	if err != nil {
		return skillToolErrorResult(call, err), nil
	}
	return tool.Result{ToolCallID: call.ID, Name: call.Name, Content: string(content)}, nil
}

func skillToolErrorResult(call tool.Call, err error) tool.Result {
	name := call.Name
	if name == "" {
		name = readSkillResourceToolName
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       name,
		Content:    err.Error(),
		IsError:    true,
	}
}

func decodeSkillToolArguments(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("agent: invalid skill tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("agent: invalid skill tool arguments: trailing JSON")
	}
	return nil
}

func renderSkillCatalog(skills []skill.Skill) string {
	if len(skills) == 0 {
		return ""
	}
	ordered := append([]skill.Skill(nil), skills...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	var b strings.Builder
	b.WriteString("Available Venat skills:\n")
	b.WriteString("When a task matches a description, call ")
	b.WriteString(activateSkillToolName)
	b.WriteString(" before proceeding. Skill resources remain unavailable until activation.\n")
	for _, current := range ordered {
		b.WriteString("- ")
		b.WriteString(current.Name)
		b.WriteString(": ")
		b.WriteString(current.Description)
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
