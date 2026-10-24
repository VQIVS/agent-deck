package ui

import (
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

func (m *Model) workflowResetAction(action string) bool {
	if action != "member-workflow-reset" || m.opts.ReadOnly {
		return false
	}
	m.teams.Confirm = "workflow-reset"
	return true
}

func (m *Model) resetWorkflowBinding(runtime TeamChannel, manager *team.Manager, index int) error {
	workflow, ok := runtime.(workflowReset)
	if !ok {
		return fmt.Errorf("workflow runtime unavailable")
	}
	if err := workflow.ResetWorkflow(m.context(), manager.Team.Members[index].Name); err != nil {
		return fmt.Errorf("reset workflow identity: %w", err)
	}
	var err error
	manager.Team, manager.Version, err = manager.Store.Load(m.context(), manager.Team.Name)
	if err != nil {
		return fmt.Errorf("reload reset workflow: %w", err)
	}
	return nil
}
