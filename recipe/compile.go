package recipe

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

func Compile(spec Spec) (Compiled, error) {
	plan, err := CompilePlan(spec)
	if err != nil {
		return Compiled{}, err
	}
	request, err := CompileStartTeamRequest(spec)
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{Request: request, Plan: plan}, nil
}

func CompilePlan(spec Spec) (planner.Plan, error) {
	tasks, err := compileTasks(spec)
	if err != nil {
		return planner.Plan{}, err
	}
	return planner.Plan{
		Goal:               stringValue(spec.Input, "query"),
		Tasks:              tasks,
		SuccessCriteria:    append([]string{}, spec.SuccessCriteria...),
		VerificationPolicy: spec.VerificationPolicy,
		Metadata:           cloneStringMap(spec.Metadata),
	}, nil
}

func CompileStartTeamRequest(spec Spec) (host.StartTeamRequest, error) {
	if strings.TrimSpace(spec.Pattern) == "" {
		return host.StartTeamRequest{}, fmt.Errorf("recipe pattern is required")
	}
	if strings.TrimSpace(spec.SupervisorProfile) == "" {
		return host.StartTeamRequest{}, fmt.Errorf("recipe supervisor_profile is required")
	}
	if len(spec.WorkerProfiles) == 0 {
		return host.StartTeamRequest{}, fmt.Errorf("recipe worker_profiles must not be empty")
	}
	return host.StartTeamRequest{
		Pattern:           spec.Pattern,
		Planner:           spec.Planner,
		SupervisorProfile: spec.SupervisorProfile,
		WorkerProfiles:    append([]string{}, spec.WorkerProfiles...),
		Input:             cloneAnyMap(spec.Input),
		Metadata:          cloneStringMap(spec.Metadata),
	}, nil
}

func compileTasks(spec Spec) ([]planner.TaskSpec, error) {
	if len(spec.Flow) > 0 {
		tasks, _, err := compileFlow(spec.Flow, nil)
		return tasks, err
	}
	tasks := make([]planner.TaskSpec, 0, len(spec.Tasks))
	for _, task := range spec.Tasks {
		tasks = append(tasks, task.toPlannerTask(nil))
	}
	return tasks, nil
}

func compileFlow(steps []Step, incoming []string) ([]planner.TaskSpec, []string, error) {
	tasks := []planner.TaskSpec{}
	terminals := append([]string{}, incoming...)
	if len(terminals) == 0 {
		terminals = nil
	}
	for _, step := range steps {
		mode := step.Mode
		if mode == "" {
			mode = "task"
		}
		var next []planner.TaskSpec
		var out []string
		var err error
		switch mode {
		case "task":
			task := step.Task
			if task == nil {
				task = step.inlineTask()
			}
			if task == nil {
				return nil, nil, fmt.Errorf("task step missing task body")
			}
			next = []planner.TaskSpec{task.toPlannerTask(terminals)}
			out = []string{task.ID}
		case "sequential":
			next, out, err = compileSequential(step.Steps, terminals)
		case "parallel":
			next, out, err = compileParallel(step.Steps, terminals)
		case "loop":
			next, out, err = compileLoop(step, terminals)
		case "tool":
			task := step.toolTask()
			next = []planner.TaskSpec{task.toPlannerTask(terminals)}
			out = []string{task.ID}
		default:
			return nil, nil, fmt.Errorf("unsupported recipe step mode: %s", mode)
		}
		if err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, next...)
		terminals = out
	}
	return tasks, terminals, nil
}

func compileSequential(steps []Step, incoming []string) ([]planner.TaskSpec, []string, error) {
	all := []planner.TaskSpec{}
	terminals := append([]string{}, incoming...)
	for _, child := range steps {
		next, out, err := compileFlow([]Step{child}, terminals)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, next...)
		terminals = out
	}
	return all, terminals, nil
}

func compileParallel(steps []Step, incoming []string) ([]planner.TaskSpec, []string, error) {
	all := []planner.TaskSpec{}
	terminals := []string{}
	for _, child := range steps {
		next, out, err := compileFlow([]Step{child}, incoming)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, next...)
		terminals = append(terminals, out...)
	}
	return all, uniqueStrings(terminals), nil
}

func compileLoop(step Step, incoming []string) ([]planner.TaskSpec, []string, error) {
	if step.Template == nil {
		return nil, nil, fmt.Errorf("loop step missing template")
	}
	all := []planner.TaskSpec{}
	terminals := []string{}
	currentDeps := append([]string{}, incoming...)
	for idx, item := range step.ForEach {
		task := substituteTask(*step.Template, item, idx+1)
		next := task.toPlannerTask(currentDeps)
		all = append(all, next)
		if step.Sequential {
			currentDeps = []string{next.ID}
			terminals = []string{next.ID}
			continue
		}
		terminals = append(terminals, next.ID)
	}
	return all, uniqueStrings(terminals), nil
}

func substituteTask(task Task, item string, index int) Task {
	replacer := strings.NewReplacer("{{item}}", item, "{{index}}", fmt.Sprintf("%d", index))
	task.ID = replacer.Replace(task.ID)
	task.Title = replacer.Replace(task.Title)
	task.Input = replacer.Replace(task.Input)
	task.AssigneeAgentID = replacer.Replace(task.AssigneeAgentID)
	task.DependsOn = replaceAll(task.DependsOn, replacer)
	task.Reads = replaceAll(task.Reads, replacer)
	task.Writes = replaceAll(task.Writes, replacer)
	task.VerifyClaims = replaceAll(task.VerifyClaims, replacer)
	task.RequiredCapabilities = replaceAll(task.RequiredCapabilities, replacer)
	return task
}

func (s Step) inlineTask() *Task {
	if s.ID == "" && s.Input == "" && s.Title == "" {
		return nil
	}
	return &Task{
		ID:                   s.ID,
		Kind:                 s.Kind,
		Title:                s.Title,
		Input:                s.Input,
		RequiredRole:         s.RequiredRole,
		RequiredCapabilities: append([]string{}, s.RequiredCapabilities...),
		AssigneeAgentID:      s.AssigneeAgentID,
		DependsOn:            append([]string{}, s.DependsOn...),
		Reads:                append([]string{}, s.Reads...),
		Writes:               append([]string{}, s.Writes...),
		Publish:              append([]team.OutputVisibility{}, s.Publish...),
		VerifyClaims:         append([]string{}, s.VerifyClaims...),
		ExchangeSchema:       s.ExchangeSchema,
		FailurePolicy:        s.FailurePolicy,
	}
}

func (s Step) toolTask() Task {
	task := Task{
		ID:                   s.ID,
		Kind:                 s.Kind,
		Title:                s.Title,
		Input:                s.Input,
		RequiredRole:         s.RequiredRole,
		RequiredCapabilities: append([]string{}, s.RequiredCapabilities...),
		AssigneeAgentID:      s.AssigneeAgentID,
		DependsOn:            append([]string{}, s.DependsOn...),
		Reads:                append([]string{}, s.Reads...),
		Writes:               append([]string{}, s.Writes...),
		Publish:              append([]team.OutputVisibility{}, s.Publish...),
		VerifyClaims:         append([]string{}, s.VerifyClaims...),
		ExchangeSchema:       s.ExchangeSchema,
		FailurePolicy:        s.FailurePolicy,
	}
	if task.ID == "" {
		task.ID = "tool-" + s.Tool
	}
	if task.Kind == "" {
		task.Kind = string(team.TaskKindResearch)
	}
	if task.Title == "" {
		task.Title = s.Tool
	}
	if task.RequiredRole == "" {
		task.RequiredRole = team.RoleSupervisor
	}
	if task.AssigneeAgentID == "" {
		task.AssigneeAgentID = "supervisor"
	}
	if len(task.RequiredCapabilities) == 0 && s.Tool != "" {
		task.RequiredCapabilities = []string{s.Tool}
	}
	if task.FailurePolicy == "" {
		task.FailurePolicy = team.FailurePolicyFailFast
	}
	return task
}

func (t Task) toPlannerTask(incoming []string) planner.TaskSpec {
	dependsOn := uniqueStrings(append(append([]string{}, incoming...), t.DependsOn...))
	return planner.TaskSpec{
		ID:                   t.ID,
		Kind:                 t.Kind,
		Title:                t.Title,
		Input:                t.Input,
		RequiredRole:         t.RequiredRole,
		RequiredCapabilities: append([]string{}, t.RequiredCapabilities...),
		Budget:               t.Budget,
		AssigneeAgentID:      t.AssigneeAgentID,
		DependsOn:            dependsOn,
		Reads:                append([]string{}, t.Reads...),
		Writes:               append([]string{}, t.Writes...),
		Publish:              append([]team.OutputVisibility{}, t.Publish...),
		VerifyClaims:         append([]string{}, t.VerifyClaims...),
		ExchangeSchema:       t.ExchangeSchema,
		FailurePolicy:        t.FailurePolicy,
	}
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func replaceAll(items []string, replacer *strings.Replacer) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, replacer.Replace(item))
	}
	return result
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func stringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	text, _ := values[key].(string)
	return text
}
