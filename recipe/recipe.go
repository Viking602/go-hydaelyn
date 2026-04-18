package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
	"gopkg.in/yaml.v3"
)

type Spec struct {
	Name               string                     `json:"name,omitempty" yaml:"name,omitempty"`
	Pattern            string                     `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Planner            string                     `json:"planner,omitempty" yaml:"planner,omitempty"`
	SupervisorProfile  string                     `json:"supervisorProfile,omitempty" yaml:"supervisor_profile,omitempty"`
	WorkerProfiles     []string                   `json:"workerProfiles,omitempty" yaml:"worker_profiles,omitempty"`
	Input              map[string]any             `json:"input,omitempty" yaml:"input,omitempty"`
	Metadata           map[string]string          `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	SuccessCriteria    []string                   `json:"successCriteria,omitempty" yaml:"success_criteria,omitempty"`
	VerificationPolicy planner.VerificationPolicy `json:"verificationPolicy,omitempty" yaml:"verification_policy,omitempty"`
	Tasks              []Task                     `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Flow               []Step                     `json:"flow,omitempty" yaml:"flow,omitempty"`
}

type Task struct {
	ID                   string                  `json:"id" yaml:"id"`
	Kind                 string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Title                string                  `json:"title,omitempty" yaml:"title,omitempty"`
	Input                string                  `json:"input,omitempty" yaml:"input,omitempty"`
	RequiredRole         team.Role               `json:"requiredRole,omitempty" yaml:"required_role,omitempty"`
	RequiredCapabilities []string                `json:"requiredCapabilities,omitempty" yaml:"required_capabilities,omitempty"`
	Budget               team.Budget             `json:"budget,omitempty" yaml:"budget,omitempty"`
	AssigneeAgentID      string                  `json:"assigneeAgentId,omitempty" yaml:"assignee_agent_id,omitempty"`
	DependsOn            []string                `json:"dependsOn,omitempty" yaml:"depends_on,omitempty"`
	Reads                []string                `json:"reads,omitempty" yaml:"reads,omitempty"`
	Writes               []string                `json:"writes,omitempty" yaml:"writes,omitempty"`
	Publish              []team.OutputVisibility `json:"publish,omitempty" yaml:"publish,omitempty"`
	FailurePolicy        team.FailurePolicy      `json:"failurePolicy,omitempty" yaml:"failure_policy,omitempty"`
}

type Step struct {
	Mode                 string                  `json:"mode,omitempty" yaml:"mode,omitempty"`
	Task                 *Task                   `json:"task,omitempty" yaml:"task,omitempty"`
	Steps                []Step                  `json:"steps,omitempty" yaml:"steps,omitempty"`
	ForEach              []string                `json:"forEach,omitempty" yaml:"for_each,omitempty"`
	Sequential           bool                    `json:"sequential,omitempty" yaml:"sequential,omitempty"`
	Template             *Task                   `json:"template,omitempty" yaml:"template,omitempty"`
	ID                   string                  `json:"id,omitempty" yaml:"id,omitempty"`
	Title                string                  `json:"title,omitempty" yaml:"title,omitempty"`
	Input                string                  `json:"input,omitempty" yaml:"input,omitempty"`
	Kind                 string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Tool                 string                  `json:"tool,omitempty" yaml:"tool,omitempty"`
	RequiredRole         team.Role               `json:"requiredRole,omitempty" yaml:"required_role,omitempty"`
	RequiredCapabilities []string                `json:"requiredCapabilities,omitempty" yaml:"required_capabilities,omitempty"`
	AssigneeAgentID      string                  `json:"assigneeAgentId,omitempty" yaml:"assignee_agent_id,omitempty"`
	DependsOn            []string                `json:"dependsOn,omitempty" yaml:"depends_on,omitempty"`
	Reads                []string                `json:"reads,omitempty" yaml:"reads,omitempty"`
	Writes               []string                `json:"writes,omitempty" yaml:"writes,omitempty"`
	Publish              []team.OutputVisibility `json:"publish,omitempty" yaml:"publish,omitempty"`
	FailurePolicy        team.FailurePolicy      `json:"failurePolicy,omitempty" yaml:"failure_policy,omitempty"`
}

type Compiled struct {
	Request host.StartTeamRequest `json:"request"`
	Plan    planner.Plan          `json:"plan"`
}

type StaticPlanner struct {
	PlanSpec planner.Plan
}

func (p StaticPlanner) Plan(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
	return p.PlanSpec, nil
}

func (p StaticPlanner) Review(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
	return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
}

func (p StaticPlanner) Replan(_ context.Context, _ planner.ReplanInput) (planner.Plan, error) {
	return p.PlanSpec, nil
}

func Decode(data []byte) (Spec, error) {
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err == nil && !isEmptySpec(spec) {
		return spec, nil
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func DecodeFile(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	return Decode(data)
}

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
		FailurePolicy:        t.FailurePolicy,
	}
}

func isEmptySpec(spec Spec) bool {
	return spec.Pattern == "" && len(spec.Flow) == 0 && len(spec.Tasks) == 0
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
