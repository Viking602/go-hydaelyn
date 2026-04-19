package recipe

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

type StrictDataflowIssueCode string

const (
	IssueUnusedWrite              StrictDataflowIssueCode = "unused_write"
	IssueMissingRead              StrictDataflowIssueCode = "missing_read"
	IssueAmbiguousProducer        StrictDataflowIssueCode = "ambiguous_producer"
	IssueSynthesisReadsUnknownKey StrictDataflowIssueCode = "synthesis_reads_unknown_key"
	IssueVerifyTaskNoClaimSource  StrictDataflowIssueCode = "verify_task_has_no_claim_source"
	IssueBlackboardNoSchema       StrictDataflowIssueCode = "blackboard_publish_has_no_schema"
)

type StrictDataflowIssue struct {
	Code    StrictDataflowIssueCode `json:"code"`
	TaskID  string                  `json:"taskId,omitempty"`
	Key     string                  `json:"key,omitempty"`
	Message string                  `json:"message"`
}

type StrictDataflowReport struct {
	OK     bool                  `json:"ok"`
	Issues []StrictDataflowIssue `json:"issues,omitempty"`
}

func ValidateStrictDataflow(plan planner.Plan) StrictDataflowReport {
	writerByKey := map[string][]planner.TaskSpec{}
	readerByKey := map[string][]planner.TaskSpec{}
	issues := make([]StrictDataflowIssue, 0)

	for _, task := range plan.Tasks {
		for _, key := range uniqueNonEmpty(task.Writes) {
			writerByKey[key] = append(writerByKey[key], task)
		}
		for _, key := range uniqueNonEmpty(task.Reads) {
			readerByKey[key] = append(readerByKey[key], task)
		}
		if isVerifyTask(task) && len(uniqueNonEmpty(task.VerifyClaims)) == 0 {
			issues = append(issues, StrictDataflowIssue{
				Code:    IssueVerifyTaskNoClaimSource,
				TaskID:  task.ID,
				Message: fmt.Sprintf("verify task %s must declare verifyClaims", task.ID),
			})
		}
		if publishesToBlackboard(task) && strings.TrimSpace(task.ExchangeSchema) == "" {
			issues = append(issues, StrictDataflowIssue{
				Code:    IssueBlackboardNoSchema,
				TaskID:  task.ID,
				Message: fmt.Sprintf("task %s publishes blackboard outputs without exchangeSchema", task.ID),
			})
		}
		if isSynthesisTask(task) && len(uniqueNonEmpty(task.Reads)) == 0 {
			issues = append(issues, StrictDataflowIssue{
				Code:    IssueMissingRead,
				TaskID:  task.ID,
				Message: fmt.Sprintf("synthesis task %s must declare reads", task.ID),
			})
		}
	}

	for key, readers := range readerByKey {
		producers := writerByKey[key]
		switch len(producers) {
		case 0:
			for _, reader := range readers {
				code := IssueMissingRead
				if isSynthesisTask(reader) {
					code = IssueSynthesisReadsUnknownKey
				}
				issues = append(issues, StrictDataflowIssue{
					Code:    code,
					TaskID:  reader.ID,
					Key:     key,
					Message: fmt.Sprintf("task %s reads unknown key %q", reader.ID, key),
				})
			}
		case 1:
		default:
			taskIDs := make([]string, 0, len(producers))
			for _, producer := range producers {
				taskIDs = append(taskIDs, producer.ID)
			}
			slices.Sort(taskIDs)
			for _, reader := range readers {
				issues = append(issues, StrictDataflowIssue{
					Code:    IssueAmbiguousProducer,
					TaskID:  reader.ID,
					Key:     key,
					Message: fmt.Sprintf("task %s reads ambiguous key %q produced by %s", reader.ID, key, strings.Join(taskIDs, ", ")),
				})
			}
		}
	}

	for key, producers := range writerByKey {
		if len(readerByKey[key]) > 0 {
			continue
		}
		for _, producer := range producers {
			issues = append(issues, StrictDataflowIssue{
				Code:    IssueUnusedWrite,
				TaskID:  producer.ID,
				Key:     key,
				Message: fmt.Sprintf("task %s writes unused key %q", producer.ID, key),
			})
		}
	}

	slices.SortFunc(issues, func(left, right StrictDataflowIssue) int {
		if left.Code != right.Code {
			return strings.Compare(string(left.Code), string(right.Code))
		}
		if left.TaskID != right.TaskID {
			return strings.Compare(left.TaskID, right.TaskID)
		}
		if left.Key != right.Key {
			return strings.Compare(left.Key, right.Key)
		}
		return strings.Compare(left.Message, right.Message)
	})
	return StrictDataflowReport{
		OK:     len(issues) == 0,
		Issues: issues,
	}
}

func uniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
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

func isVerifyTask(task planner.TaskSpec) bool {
	kind := strings.ToLower(strings.TrimSpace(task.Kind))
	return kind == string(team.TaskKindVerify) || task.Stage == team.TaskStageVerify
}

func isSynthesisTask(task planner.TaskSpec) bool {
	kind := strings.ToLower(strings.TrimSpace(task.Kind))
	return kind == string(team.TaskKindSynthesize) || task.Stage == team.TaskStageSynthesize
}

func publishesToBlackboard(task planner.TaskSpec) bool {
	return slices.Contains(task.Publish, team.OutputVisibilityBlackboard)
}
