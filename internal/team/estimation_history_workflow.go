package team

import (
	"fmt"
	"strings"
)

func configuredWorkflowInstructions(t *Team) string {
	if !t.LeasePool && len(t.Workflow.Stages) == 0 {
		return ""
	}
	definition := t.Workflow.Resolved()
	var text strings.Builder
	text.WriteString("\n\nConfigured task workflow (stage names are exact; categories define behavior):\n")
	for _, stage := range definition.Stages {
		fmt.Fprintf(&text, "- %q: %s\n", stage.Name, stage.Category)
	}
	text.WriteString("Discover currently authorized moves using task_workflow (or task workflow CLI). " +
		"Use configured stage names; changing a stage name never grants acceptance or release authority. " +
		"Shipment requires previously accepted work, an explicit version, and authorized release-manager/user. " +
		"pending_release lists accepted work waiting for shipment. Completion and shipment are distinct milestones.\n")
	return text.String()
}
