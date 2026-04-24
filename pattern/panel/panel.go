package panel

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

type Pattern struct{}

func New() Pattern {
	return Pattern{}
}

func (Pattern) Name() string {
	return "panel"
}

func (Pattern) PlanTemplate(request team.StartRequest) (planner.Template, error) {
	todos := parseTodos(request.Input)
	if len(todos) == 0 {
		return planner.Template{}, fmt.Errorf("panel requires at least one todo")
	}
	tasks := make([]planner.TaskSpec, 0, len(todos))
	for _, todo := range todos {
		tasks = append(tasks, researchTaskSpec(todo))
	}
	return planner.Template{
		Name: request.Pattern,
		Goal: goalForRequest(request),
		VerificationPolicy: planner.VerificationPolicy{
			Required: verificationRequired(request.Input, todos),
			Mode:     "panel_cross_review",
		},
		Tasks: tasks,
	}, nil
}

func (Pattern) DecoratePlannedState(request team.StartRequest, plan planner.Plan, state team.RunState) (team.RunState, error) {
	todos := plannedTodos(request.Input, plan.Tasks)
	if len(todos) == 0 {
		return state, fmt.Errorf("panel requires at least one todo")
	}
	now := time.Now().UTC()
	goal := firstNonEmpty(plan.Goal, goalForRequest(request))
	board := team.TaskBoard{Plan: team.TodoPlan{
		ID:        request.TeamID + "-todo-plan",
		Goal:      goal,
		Items:     todos,
		CreatedAt: now,
		UpdatedAt: now,
	}}
	capabilities := capabilitiesForWorkers(state.Workers, request)
	for idx := range state.Tasks {
		todoID := todoIDForPlannedTask(state.Tasks[idx], todos)
		if todoID == "" {
			continue
		}
		agentID, err := claimBest(&board, todoID, capabilities)
		if err != nil {
			return state, err
		}
		state.Tasks[idx].TodoID = todoID
		state.Tasks[idx].AssigneeAgentID = agentID
		state.Tasks[idx].Assignee = agentID
		if state.Tasks[idx].ExpectedReportKind == "" {
			state.Tasks[idx].ExpectedReportKind = expectedReportKindForTask(state.Tasks[idx])
		}
		state.Tasks[idx].Normalize()
		setTodoTaskID(&board, todoID, state.Tasks[idx].ID)
	}
	state.InteractionMode = team.InteractionModePanel
	state.TaskBoard = &board
	if len(state.Threads) == 0 {
		state.Threads = initialThreadsForGoal(request.TeamID, goal, now)
	}
	return state, nil
}

func (Pattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	if len(request.WorkerProfiles) == 0 {
		return team.RunState{}, fmt.Errorf("panel requires at least one worker profile")
	}
	todos := parseTodos(request.Input)
	if len(todos) == 0 {
		return team.RunState{}, fmt.Errorf("panel requires at least one todo")
	}
	workers := instantiateWorkers(request.WorkerProfiles)
	board := team.TaskBoard{Plan: team.TodoPlan{
		ID:        request.TeamID + "-todo-plan",
		Goal:      goalForRequest(request),
		Items:     todos,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	capabilities := capabilitiesForWorkers(workers, request)
	tasks := make([]team.Task, 0, len(todos))
	for _, todo := range todos {
		agentID, err := claimBest(&board, todo.ID, capabilities)
		if err != nil {
			return team.RunState{}, err
		}
		task := researchTask(todo, agentID)
		task.Normalize()
		setTodoTaskID(&board, todo.ID, task.ID)
		tasks = append(tasks, task)
	}
	requireVerification := verificationRequired(request.Input, todos)
	now := time.Now().UTC()
	return team.RunState{
		ID:              request.TeamID,
		Pattern:         "panel",
		Status:          team.StatusRunning,
		Phase:           team.PhaseResearch,
		InteractionMode: team.InteractionModePanel,
		Supervisor: team.Member{
			ID:          "supervisor",
			Role:        team.RoleSupervisor,
			ProfileName: request.SupervisorProfile,
		},
		Workers:             workers,
		Tasks:               tasks,
		TaskBoard:           &board,
		Threads:             initialThreads(request, now),
		Input:               request.Input,
		Metadata:            request.Metadata,
		RequireVerification: requireVerification,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func (Pattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	state.UpdatedAt = time.Now().UTC()
	switch state.Phase {
	case team.PhaseResearch:
		if hasPendingStageTasks(state, team.TaskStageImplement) {
			return state, nil
		}
		if !hasStageTasks(state, team.TaskStageReview) {
			state.Tasks = append(state.Tasks, reviewTasks(state)...)
		}
		state.Phase = team.PhaseVerify
		return state, nil
	case team.PhaseVerify:
		if hasPendingStageTasks(state, team.TaskStageReview) {
			return state, nil
		}
		markReviewedTodos(&state)
		if !hasStageTasks(state, team.TaskStageSynthesize) {
			state.Tasks = append(state.Tasks, synthesizeTask(state))
		}
		state.Phase = team.PhaseSynthesize
		return state, nil
	case team.PhaseSynthesize:
		if hasPendingStageTasks(state, team.TaskStageSynthesize) {
			return state, nil
		}
		state.Result = synthesizedResult(state)
		state.Status = team.StatusCompleted
		return state, nil
	default:
		return state, nil
	}
}

func parseTodos(input map[string]any) []team.TodoItem {
	if input == nil {
		return nil
	}
	for _, key := range []string{"todos", "tasks", "subqueries", "branches"} {
		if todos := todosFromValue(input[key], boolValue(input["requireVerification"])); len(todos) > 0 {
			return todos
		}
	}
	goal := goalText(input)
	if goal == "" {
		return nil
	}
	return []team.TodoItem{newTodo(0, map[string]any{"title": goal, "input": goal}, boolValue(input["requireVerification"]))}
}

func todosFromValue(value any, defaultVerify bool) []team.TodoItem {
	switch current := value.(type) {
	case []string:
		items := make([]team.TodoItem, 0, len(current))
		for idx, text := range current {
			items = append(items, newTodo(idx, map[string]any{"title": text, "input": text}, defaultVerify))
		}
		return items
	case []any:
		items := make([]team.TodoItem, 0, len(current))
		for idx, item := range current {
			switch cast := item.(type) {
			case string:
				items = append(items, newTodo(idx, map[string]any{"title": cast, "input": cast}, defaultVerify))
			case map[string]any:
				items = append(items, newTodo(idx, cast, defaultVerify))
			}
		}
		return items
	default:
		return nil
	}
}

func plannedTodos(input map[string]any, specs []planner.TaskSpec) []team.TodoItem {
	inputTodos := parseTodos(input)
	inputByID := make(map[string]team.TodoItem, len(inputTodos))
	for _, todo := range inputTodos {
		inputByID[todo.ID] = todo
	}
	todos := make([]team.TodoItem, 0, len(specs))
	seen := map[string]struct{}{}
	for _, spec := range specs {
		if !isPlannedTodoSpec(spec) {
			continue
		}
		todoID := firstNonEmpty(spec.TodoID, spec.ID)
		if todoID == "" {
			continue
		}
		if _, ok := seen[todoID]; ok {
			continue
		}
		seen[todoID] = struct{}{}
		todo := inputByID[todoID]
		if todo.ID == "" {
			todo = team.TodoItem{ID: todoID, Status: team.TodoStatusOpen}
		}
		todo.ID = todoID
		todo.Title = firstNonEmpty(spec.Title, todo.Title, spec.Input)
		todo.Input = firstNonEmpty(spec.Input, todo.Input, todo.Title)
		if len(spec.RequiredCapabilities) > 0 {
			todo.RequiredCapabilities = append([]string{}, spec.RequiredCapabilities...)
		}
		if len(spec.DependsOn) > 0 {
			todo.Dependencies = append([]string{}, spec.DependsOn...)
		}
		if spec.ExpectedReportKind != "" {
			todo.ExpectedReportKind = spec.ExpectedReportKind
		}
		if todo.ExpectedReportKind == "" {
			todo.ExpectedReportKind = expectedReportKindForSpec(spec)
		}
		todo.VerificationPolicy.Required = todo.VerificationPolicy.Required || spec.VerifierRequired
		if todo.VerificationPolicy.Mode == "" {
			todo.VerificationPolicy.Mode = "cross_review"
		}
		todo.Status = team.TodoStatusOpen
		todos = append(todos, todo)
	}
	if len(todos) > 0 {
		return todos
	}
	return inputTodos
}

func isPlannedTodoSpec(spec planner.TaskSpec) bool {
	kind := team.TaskKind(spec.Kind)
	if kind == team.TaskKindVerify || kind == team.TaskKindSynthesize {
		return false
	}
	switch spec.Stage {
	case team.TaskStageReview, team.TaskStageVerify, team.TaskStageSynthesize:
		return false
	default:
		return true
	}
}

func newTodo(idx int, raw map[string]any, defaultVerify bool) team.TodoItem {
	id := stringValue(raw, "id")
	if id == "" {
		id = fmt.Sprintf("todo-%d", idx+1)
	}
	title := stringValue(raw, "title")
	input := stringValue(raw, "input")
	if title == "" {
		title = input
	}
	if input == "" {
		input = title
	}
	priority := team.TodoPriority(strings.ToLower(stringValue(raw, "priority")))
	if priority == "" {
		priority = team.TodoPriorityNormal
	}
	expected := team.ReportKind(stringValue(raw, "expectedReportKind"))
	if expected == "" {
		expected = team.ReportKindResearch
	}
	verify := defaultVerify || boolValue(raw["verifierRequired"])
	mode := stringValue(raw, "verificationMode")
	minConfidence := floatValue(raw["minConfidence"])
	reviewers := intValue(raw["reviewers"])
	if policy, ok := raw["verificationPolicy"].(map[string]any); ok {
		verify = verify || boolValue(policy["required"])
		mode = firstNonEmpty(stringValue(policy, "mode"), mode)
		if value := floatValue(policy["minConfidence"]); value > 0 {
			minConfidence = value
		}
		if value := intValue(policy["reviewers"]); value > 0 {
			reviewers = value
		}
	}
	return team.TodoItem{
		ID:                   id,
		Title:                title,
		Input:                input,
		Domain:               stringValue(raw, "domain"),
		RequiredCapabilities: stringSlice(raw["requiredCapabilities"]),
		Priority:             priority,
		Dependencies:         stringSlice(raw["dependencies"]),
		ExpectedReportKind:   expected,
		VerificationPolicy: team.TodoVerificationPolicy{
			Required:      verify,
			Mode:          firstNonEmpty(mode, "cross_review"),
			MinConfidence: minConfidence,
			Reviewers:     reviewers,
		},
		Status: team.TodoStatusOpen,
	}
}

func instantiateWorkers(profiles []string) []team.Member {
	workers := make([]team.Member, 0, len(profiles))
	for idx, profile := range profiles {
		workers = append(workers, team.Member{
			ID:          fmt.Sprintf("worker-%d", idx+1),
			Role:        team.RoleResearcher,
			ProfileName: profile,
		})
	}
	return workers
}

func capabilitiesForWorkers(workers []team.Member, request team.StartRequest) []team.AgentCapability {
	specs := expertSpecs(request.Input)
	items := make([]team.AgentCapability, 0, len(workers))
	for _, worker := range workers {
		spec := specs[worker.ProfileName]
		capability := team.AgentCapability{
			AgentID: worker.ID,
			Roles:   []team.Role{worker.Role},
			Domains: append([]string{}, spec.domains...),
			Tools:   append([]string{}, spec.capabilities...),
		}
		if len(capability.Domains) == 0 {
			if strings.TrimSpace(worker.ProfileName) != "" {
				capability.Domains = []string{worker.ProfileName}
			} else {
				capability.Domains = []string{"*"}
			}
		}
		items = append(items, capability)
	}
	return items
}

type expertSpec struct {
	domains      []string
	capabilities []string
}

func expertSpecs(input map[string]any) map[string]expertSpec {
	specs := map[string]expertSpec{}
	if input == nil {
		return specs
	}
	raw, _ := input["experts"].([]any)
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		profile := stringValue(m, "profile")
		if profile == "" {
			profile = stringValue(m, "profileName")
		}
		if profile == "" {
			continue
		}
		specs[profile] = expertSpec{
			domains:      stringSlice(m["domains"]),
			capabilities: stringSlice(m["capabilities"]),
		}
	}
	return specs
}

func claimBest(board *team.TaskBoard, todoID string, agents []team.AgentCapability) (string, error) {
	var claimErr error
	for _, agent := range agents {
		claimed, err := board.Claim(todoID, agent, team.ClaimOptions{RequireDomainMatch: true})
		if err == nil {
			return claimed.PrimaryAgentID, nil
		}
		claimErr = err
	}
	if claimErr == nil {
		claimErr = fmt.Errorf("no panel expert available")
	}
	return "", claimErr
}

func researchTask(todo team.TodoItem, assignee string) team.Task {
	return team.Task{
		ID:                   todo.ID,
		TodoID:               todo.ID,
		Kind:                 team.TaskKindResearch,
		Stage:                team.TaskStageImplement,
		Title:                todo.Title,
		Input:                todo.Input,
		RequiredRole:         team.RoleResearcher,
		RequiredCapabilities: append([]string{}, todo.RequiredCapabilities...),
		AssigneeAgentID:      assignee,
		DependsOn:            append([]string{}, todo.Dependencies...),
		Writes:               []string{researchWriteKey(todo.ID)},
		Publish:              []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		Namespace:            researchNamespace(todo.ID),
		VerifierRequired:     todo.VerificationPolicy.Required,
		ExpectedReportKind:   team.ReportKindResearch,
		FailurePolicy:        team.FailurePolicyFailFast,
		Status:               team.TaskStatusPending,
	}
}

func todoIDForPlannedTask(task team.Task, todos []team.TodoItem) string {
	if !isPlannedTodoTask(task) {
		return ""
	}
	if task.TodoID != "" && todoExists(todos, task.TodoID) {
		return task.TodoID
	}
	if todoExists(todos, task.ID) {
		return task.ID
	}
	return ""
}

func isPlannedTodoTask(task team.Task) bool {
	if task.Kind == team.TaskKindVerify || task.Kind == team.TaskKindSynthesize {
		return false
	}
	switch task.Stage {
	case team.TaskStageReview, team.TaskStageVerify, team.TaskStageSynthesize:
		return false
	default:
		return true
	}
}

func todoExists(todos []team.TodoItem, todoID string) bool {
	for _, todo := range todos {
		if todo.ID == todoID {
			return true
		}
	}
	return false
}

func expectedReportKindForTask(task team.Task) team.ReportKind {
	switch task.Kind {
	case team.TaskKindVerify:
		return team.ReportKindVerification
	case team.TaskKindSynthesize:
		return team.ReportKindSynthesis
	default:
		return team.ReportKindResearch
	}
}

func expectedReportKindForSpec(spec planner.TaskSpec) team.ReportKind {
	switch team.TaskKind(spec.Kind) {
	case team.TaskKindVerify:
		return team.ReportKindVerification
	case team.TaskKindSynthesize:
		return team.ReportKindSynthesis
	default:
		return team.ReportKindResearch
	}
}

func researchTaskSpec(todo team.TodoItem) planner.TaskSpec {
	task := researchTask(todo, "")
	return planner.TaskSpec{
		ID:                   task.ID,
		TodoID:               task.TodoID,
		Kind:                 string(task.Kind),
		Stage:                task.Stage,
		Title:                task.Title,
		Input:                task.Input,
		RequiredRole:         task.RequiredRole,
		RequiredCapabilities: append([]string{}, task.RequiredCapabilities...),
		DependsOn:            append([]string{}, task.DependsOn...),
		Writes:               append([]string{}, task.Writes...),
		Publish:              append([]team.OutputVisibility{}, task.Publish...),
		Namespace:            task.Namespace,
		VerifierRequired:     task.VerifierRequired,
		ExpectedReportKind:   task.ExpectedReportKind,
		FailurePolicy:        task.FailurePolicy,
	}
}

func reviewTasks(state team.RunState) []team.Task {
	items := make([]team.Task, 0)
	for _, task := range state.Tasks {
		if task.Stage != team.TaskStageImplement || task.Status != team.TaskStatusCompleted {
			continue
		}
		reviewers := selectReviewers(state, task.EffectiveAssigneeAgentID(), reviewerCount(state.TaskBoard, task.TodoID))
		reviewIDs := make([]string, 0, len(reviewers))
		for idx, reviewer := range reviewers {
			review := team.Task{
				ID:                 reviewTaskID(task.ID, idx),
				TodoID:             task.TodoID,
				Kind:               team.TaskKindVerify,
				Stage:              team.TaskStageReview,
				Title:              "review " + task.Title,
				Input:              "Review the panel todo output and verify each claim with per-claim status.",
				RequiredRole:       team.RoleResearcher,
				AssigneeAgentID:    reviewer,
				DependsOn:          []string{task.ID},
				ReadSelectors:      []blackboard.ExchangeSelector{reviewSelector(task.ID)},
				Writes:             []string{reviewWriteKey(task.TodoID)},
				Publish:            []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
				Namespace:          reviewNamespace(task.TodoID),
				VerifierRequired:   true,
				ExpectedReportKind: team.ReportKindVerification,
				FailurePolicy:      task.FailurePolicy,
				Status:             team.TaskStatusPending,
			}
			review.Normalize()
			reviewIDs = append(reviewIDs, review.ID)
			items = append(items, review)
		}
		if state.TaskBoard != nil {
			_ = state.TaskBoard.SetReviewers(task.TodoID, reviewers, reviewIDs)
		}
	}
	return items
}

func reviewSelector(taskID string) blackboard.ExchangeSelector {
	return blackboard.ExchangeSelector{
		TaskIDs:           []string{taskID},
		IncludeText:       true,
		IncludeStructured: true,
		IncludeArtifacts:  true,
		Required:          true,
		Label:             "panel research output for " + taskID,
	}
}

func synthesizeTask(state team.RunState) team.Task {
	deps := stageDependencies(state, team.TaskStageReview)
	task := team.Task{
		ID:                 "task-synthesize",
		Kind:               team.TaskKindSynthesize,
		Stage:              team.TaskStageSynthesize,
		Title:              "synthesize panel answer",
		Input:              "Synthesize the panel verified findings into a complete answer with adopted and excluded evidence.",
		RequiredRole:       team.RoleSupervisor,
		AssigneeAgentID:    state.Supervisor.ID,
		DependsOn:          deps,
		ReadSelectors:      []blackboard.ExchangeSelector{verifiedFindingsSelector()},
		Publish:            []team.OutputVisibility{team.OutputVisibilityShared},
		Namespace:          "panel.synthesize",
		VerifierRequired:   true,
		ExpectedReportKind: team.ReportKindSynthesis,
		FailurePolicy:      team.FailurePolicyFailFast,
		Status:             team.TaskStatusPending,
	}
	task.Normalize()
	return task
}

func verifiedFindingsSelector() blackboard.ExchangeSelector {
	return blackboard.ExchangeSelector{
		Keys:              []string{"supported_findings"},
		RequireVerified:   true,
		IncludeText:       true,
		IncludeStructured: true,
		IncludeArtifacts:  true,
		Required:          true,
		Label:             "panel verified findings",
	}
}

func synthesizedResult(state team.RunState) *team.Result {
	task := latestCompletedStageTask(state, team.TaskStageSynthesize)
	if task == nil || task.Result == nil {
		return &team.Result{}
	}
	result := cloneResult(*task.Result)
	if result.Structured == nil {
		result.Structured = map[string]any{}
	}
	result.Structured["panel"] = panelEvidenceTrail(state)
	if state.Blackboard != nil {
		for _, finding := range state.Blackboard.SupportedFindings() {
			result.Findings = append(result.Findings, team.Finding{Summary: finding.Summary, Confidence: finding.Confidence})
		}
	}
	return &result
}

func panelEvidenceTrail(state team.RunState) map[string]any {
	out := map[string]any{
		"todos":        todoPayload(state.TaskBoard),
		"participants": participantPayload(state.Workers),
	}
	if state.Blackboard == nil {
		out["adoptedFindings"] = []map[string]any{}
		out["excludedClaims"] = []map[string]any{}
		out["evidence"] = []map[string]any{}
		return out
	}
	out["adoptedFindings"] = adoptedFindingPayload(state.Blackboard.SupportedFindings())
	out["excludedClaims"] = excludedClaimPayload(state.Blackboard)
	out["evidence"] = evidencePayload(state.Blackboard.Evidence)
	return out
}

func todoPayload(board *team.TaskBoard) []map[string]any {
	if board == nil {
		return nil
	}
	items := make([]map[string]any, 0, len(board.Plan.Items))
	for _, todo := range board.Plan.Items {
		items = append(items, map[string]any{
			"id":             todo.ID,
			"title":          todo.Title,
			"domain":         todo.Domain,
			"status":         string(todo.Status),
			"primaryAgentId": todo.PrimaryAgentID,
			"reviewerIds":    append([]string{}, todo.ReviewerAgentIDs...),
		})
	}
	return items
}

func participantPayload(workers []team.AgentInstance) []map[string]any {
	items := make([]map[string]any, 0, len(workers))
	for _, worker := range workers {
		items = append(items, map[string]any{
			"id":          worker.ID,
			"role":        string(worker.Role),
			"profileName": worker.ProfileName,
		})
	}
	return items
}

func adoptedFindingPayload(findings []blackboard.Finding) []map[string]any {
	items := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		items = append(items, map[string]any{
			"id":          finding.ID,
			"summary":     finding.Summary,
			"claimIds":    append([]string{}, finding.ClaimIDs...),
			"evidenceIds": append([]string{}, finding.EvidenceIDs...),
			"confidence":  finding.Confidence,
		})
	}
	return items
}

func excludedClaimPayload(board *blackboard.State) []map[string]any {
	if board == nil {
		return nil
	}
	verifications := map[string]blackboard.VerificationResult{}
	for _, item := range board.Verifications {
		verifications[item.ClaimID] = item
	}
	items := make([]map[string]any, 0)
	for _, claim := range board.Claims {
		verification, ok := verifications[claim.ID]
		if !ok || verification.SupportsClaim(blackboard.DefaultVerificationConfidence) {
			continue
		}
		items = append(items, map[string]any{
			"id":          claim.ID,
			"summary":     claim.Summary,
			"status":      string(verification.Status),
			"confidence":  verification.Confidence,
			"evidenceIds": append([]string{}, verification.EvidenceIDs...),
			"reason":      verification.Rationale,
		})
	}
	return items
}

func evidencePayload(evidence []blackboard.Evidence) []map[string]any {
	items := make([]map[string]any, 0, len(evidence))
	for _, item := range evidence {
		items = append(items, map[string]any{
			"id":      item.ID,
			"source":  item.SourceID,
			"snippet": item.Snippet,
			"score":   item.Score,
		})
	}
	return items
}

func initialThreads(request team.StartRequest, now time.Time) []team.ConversationThread {
	return initialThreadsForGoal(request.TeamID, goalForRequest(request), now)
}

func initialThreadsForGoal(teamID, goal string, now time.Time) []team.ConversationThread {
	return []team.ConversationThread{{
		ID:        teamID + "-panel",
		TeamID:    teamID,
		Topic:     goal,
		Mode:      team.InteractionModePanel,
		CreatedAt: now,
		UpdatedAt: now,
	}}
}

func markReviewedTodos(state *team.RunState) {
	if state.TaskBoard == nil {
		return
	}
	for _, task := range state.Tasks {
		if task.Stage == team.TaskStageReview && task.Status == team.TaskStatusCompleted {
			_ = state.TaskBoard.SetStatus(task.TodoID, team.TodoStatusVerified)
		}
	}
}

func setTodoTaskID(board *team.TaskBoard, todoID, taskID string) {
	if board == nil {
		return
	}
	for idx := range board.Plan.Items {
		if board.Plan.Items[idx].ID == todoID {
			board.Plan.Items[idx].TaskID = taskID
			board.Plan.Items[idx].UpdatedAt = time.Now().UTC()
			board.Plan.UpdatedAt = board.Plan.Items[idx].UpdatedAt
			return
		}
	}
}

func hasPendingStageTasks(state team.RunState, stage team.TaskStage) bool {
	for _, task := range state.Tasks {
		if task.Stage != stage {
			continue
		}
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return true
		}
	}
	return false
}

func hasStageTasks(state team.RunState, stage team.TaskStage) bool {
	for _, task := range state.Tasks {
		if task.Stage == stage {
			return true
		}
	}
	return false
}

func stageDependencies(state team.RunState, stage team.TaskStage) []string {
	items := make([]string, 0)
	for _, task := range state.Tasks {
		if task.Stage == stage {
			items = append(items, task.ID)
		}
	}
	return items
}

func latestCompletedStageTask(state team.RunState, stage team.TaskStage) *team.Task {
	for idx := len(state.Tasks) - 1; idx >= 0; idx-- {
		if state.Tasks[idx].Stage == stage && state.Tasks[idx].Status == team.TaskStatusCompleted {
			return &state.Tasks[idx]
		}
	}
	return nil
}

func reviewerCount(board *team.TaskBoard, todoID string) int {
	count := 1
	if board == nil {
		return count
	}
	for _, todo := range board.Plan.Items {
		if todo.ID == todoID && todo.VerificationPolicy.Reviewers > 0 {
			return todo.VerificationPolicy.Reviewers
		}
	}
	return count
}

func selectReviewers(state team.RunState, primary string, count int) []string {
	if count <= 0 {
		count = 1
	}
	candidates := make([]string, 0, len(state.Workers))
	for _, worker := range state.Workers {
		if worker.ID != primary {
			candidates = append(candidates, worker.ID)
		}
	}
	if primary != "" {
		candidates = append(candidates, primary)
	}
	if len(candidates) == 0 {
		return nil
	}
	reviewers := make([]string, 0, count)
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		reviewers = append(reviewers, candidate)
		if len(reviewers) == count {
			break
		}
	}
	return reviewers
}

func reviewTaskID(taskID string, idx int) string {
	if idx <= 0 {
		return taskID + "-review"
	}
	return fmt.Sprintf("%s-review-%d", taskID, idx+1)
}

func researchWriteKey(todoID string) string {
	return "panel.research." + todoID
}

func reviewWriteKey(todoID string) string {
	return "panel.review." + todoID
}

func researchNamespace(todoID string) string {
	return "panel.todo." + todoID
}

func reviewNamespace(todoID string) string {
	return "panel.review." + todoID
}

func baseTodoID(todoID string) string {
	return todoID
}

func goalForRequest(request team.StartRequest) string {
	return goalText(request.Input)
}

func goalText(input map[string]any) string {
	for _, key := range []string{"query", "goal"} {
		if text := stringValue(input, key); text != "" {
			return text
		}
	}
	return ""
}

func verificationRequired(input map[string]any, todos []team.TodoItem) bool {
	if boolValue(input["requireVerification"]) {
		return true
	}
	for _, todo := range todos {
		if todo.VerificationPolicy.Required {
			return true
		}
	}
	return false
}

func cloneResult(result team.Result) team.Result {
	clone := result
	clone.Structured = cloneStructured(result.Structured)
	clone.ArtifactIDs = append([]string{}, result.ArtifactIDs...)
	clone.Findings = append([]team.Finding{}, result.Findings...)
	clone.Evidence = append([]team.Evidence{}, result.Evidence...)
	return clone
}

func cloneStructured(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	maps.Copy(clone, values)
	return clone
}

func stringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	text, _ := values[key].(string)
	return strings.TrimSpace(text)
}

func stringSlice(value any) []string {
	switch current := value.(type) {
	case []string:
		return append([]string{}, current...)
	case []any:
		items := make([]string, 0, len(current))
		for _, item := range current {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				items = append(items, strings.TrimSpace(text))
			}
		}
		return items
	default:
		return nil
	}
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func floatValue(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	default:
		return 0
	}
}

func intValue(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case float64:
		return int(current)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
