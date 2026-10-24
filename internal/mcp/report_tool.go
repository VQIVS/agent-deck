package mcp

// SubmitReportTool describes worker claims only; trusted policy and metadata cannot be supplied by callers.
func SubmitReportTool() map[string]any {
	text := map[string]any{"type": "string"}
	object := func(properties map[string]any, required []string) map[string]any {
		return map[string]any{
			"type": "object", "properties": properties, "required": required, "additionalProperties": false,
		}
	}
	array := func(items map[string]any) map[string]any {
		return map[string]any{"type": "array", "items": items, "maxItems": 100}
	}
	return tool("submit_report", "Persist a worker-reported checklist and enqueue a private master notification",
		map[string]any{
			"member": text, "session": text, "summary": text,
			"checklist": array(object(map[string]any{
				"id": text, "complete": map[string]any{"type": "boolean"}, "evidence": text,
			}, []string{"id", "complete"})),
			"tests": array(object(map[string]any{
				"criterion_id": text, "command": text, "details": text,
				"outcome": map[string]any{"type": "string", "enum": []string{"pass", "fail", "not_run"}},
			}, []string{"criterion_id", "command", "outcome"})),
			"open_issues": array(text),
		}, []string{"member", "session", "summary", "checklist"})
}
