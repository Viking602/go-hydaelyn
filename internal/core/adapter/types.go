// Package adapter is the only bidirectional bridge between the public
// api types and the internal core model. Converters are grouped by
// domain: types_run_task.go (run/task/envelope), types_blackboard_event.go
// (blackboard/event/lease/resume-token), types_governance_pipeline.go
// (handoff/approval/policy/pipeline/projection), types_message_trace.go
// (user message/action/tool/profile/trace), types_selector_catalog.go
// (selectors/usage/dead-letter/capability). interfaces_pipeline.go adapts
// pipeline and policy contracts; interfaces_store_api.go and
// interfaces_store_core.go adapt the store contracts in each direction.
// This file keeps only the shared clone helpers.
package adapter

func stringMapToModel(in map[string]string) map[string]string   { return cloneStringMap(in) }
func stringMapFromModel(in map[string]string) map[string]string { return cloneStringMap(in) }
func anyMapToModel(in map[string]any) map[string]any            { return cloneAnyMap(in) }
func anyMapFromModel(in map[string]any) map[string]any          { return cloneAnyMap(in) }

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	return append([]byte(nil), in...)
}
