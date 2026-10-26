package team

import (
	"context"
	"fmt"
)

// RenameStage remaps current placement and transition references without rewriting historical evidence.
func (b *TaskBoard) RenameStage(old, name string, mutation TaskMutation) error {
	if err := mutation.validate(); err != nil {
		return err
	}
	if !mutation.manages() {
		return fmt.Errorf("workflow rename requires master or user")
	}
	candidate := cloneBoard(b)
	candidate.Workflow = candidate.Workflow.Resolved().Clone()
	found := false
	for i := range candidate.Workflow.Stages {
		if candidate.Workflow.Stages[i].Name == old {
			candidate.Workflow.Stages[i].Name, found = name, true
		}
	}
	if !found {
		return fmt.Errorf("unknown workflow stage %q", old)
	}
	rules := make(map[string]WorkflowTransition, len(candidate.Workflow.Transitions))
	for target, rule := range candidate.Workflow.Transitions {
		if target == old {
			target = name
		}
		for i := range rule.From {
			if rule.From[i] == old {
				rule.From[i] = name
			}
		}
		rules[target] = rule
	}
	candidate.Workflow.Transitions = rules
	if err := candidate.Workflow.Validate(); err != nil {
		return err
	}
	for i := range candidate.Tasks {
		task := &candidate.Tasks[i]
		if task.State == old {
			task.State = name
			bindTaskCategory(&candidate, task)
		}
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*b = candidate
	return nil
}

// ValidMoves previews allowed destinations without inventing release metadata or changing task state.
func (b *TaskBoard) ValidMoves(id string, mutation TaskMutation) []WorkflowStage {
	mutation.ReadOnly = false
	var stages []WorkflowStage
	for _, stage := range b.Workflow.Resolved().Stages {
		if stage.Category == "shipped" {
			task, err := taskByID(b, id)
			if err == nil && mutation.validate() == nil && taskCategory(task) == "done" && taskCompletionValid(task) &&
				!mutation.Actor.Automation && (mutation.Actor.User || mutation.Actor.Member == "release-manager") &&
				b.authorizeTransition(task.State, stage.Name, mutation) == nil {
				stages = append(stages, stage)
			}
		} else if b.CanTransition(id, stage.Name, mutation) == nil {
			stages = append(stages, stage)
		}
	}
	return stages
}

// SaveWorkflowRenamed includes explicit old-name to new-name remaps in the coordinated save.
func SaveWorkflowRenamed(ctx context.Context, manager *Manager, store BoardStore, definition WorkflowDefinition,
	renames map[string]string, expected uint64, mutation TaskMutation,
) error {
	if manager == nil || manager.ReadOnly || store.ReadOnly {
		return fmt.Errorf("workflow save requires writable manager")
	}
	if err := mutation.validate(); err != nil {
		return err
	}
	if !mutation.manages() {
		return fmt.Errorf("workflow save requires master or user")
	}
	return store.Exclusive(ctx, func() error {
		state, err := store.LoadMigratedUnlocked(ctx)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBoardChanged
		}
		for old, name := range renames {
			if err := state.Board.RenameStage(old, name, mutation); err != nil {
				return err
			}
		}
		if err := state.Board.SetWorkflow(definition, mutation); err != nil {
			return err
		}
		previous := manager.Team.Workflow
		manager.Team.Workflow = definition.Resolved().Clone()
		if err := manager.Save(ctx); err != nil {
			manager.Team.Workflow = previous
			return err
		}
		state.Revision++
		state.Approved = map[string]bool{}
		if err := store.write(ctx, "board.json", &state); err != nil {
			return fmt.Errorf("team workflow saved but board workflow pending; reload and retry to repair: %w", err)
		}
		return nil
	})
}
