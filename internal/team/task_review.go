package team

import (
	"fmt"
	"slices"
	"time"
)

// ReviewTaskForTeam validates current reviewer membership before applying a W1 review transition.
// User is privileged integration input and must never come from tool arguments.
// Acceptance is an explicit manual review decision against merged task/team DoD; a worker report is not required.
// The decision records reviewer acceptance, not an independently observed passing test outcome.
func ReviewTaskForTeam(board *TaskBoard, current *Team, taskID, authenticatedMember, decision, comment string,
	user bool, now time.Time, readOnly bool,
) error {
	if board == nil || current == nil {
		return fmt.Errorf("review requires current team and board")
	}
	task, err := taskByID(board, taskID)
	if err != nil {
		return err
	}
	actor := TaskActor{User: user}
	if !user {
		actor, err = current.ResolveTaskActor(authenticatedMember)
		if err != nil {
			return err
		}
		if task.Reviewer != actor.Member || task.Assignee == actor.Member {
			return fmt.Errorf("review requires current assigned independent reviewer")
		}
	}
	if _, err := TaskReviewDefinition(current, task); err != nil {
		return err
	}
	return board.ReviewTask(taskID, decision, comment, TaskMutation{Actor: actor, Now: now, ReadOnly: readOnly})
}

// TaskReviewDefinition merges task acceptance with current team DoD without weakening either requirement.
// Task criteria retain their order; additional team criteria follow in configured order.
func TaskReviewDefinition(current *Team, task *Task) (DefinitionOfDone, error) {
	if current == nil || task == nil {
		return nil, fmt.Errorf("review definition requires current team and task")
	}
	definition := taskDefinition(task)
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	if len(current.DefinitionOfDone) == 0 {
		return definition, nil
	}
	if err := current.DefinitionOfDone.Validate(); err != nil {
		return nil, err
	}
	for _, criterion := range current.DefinitionOfDone {
		index := slices.IndexFunc(definition, func(item DoneCriterion) bool { return item.ID == criterion.ID })
		if index < 0 {
			definition = append(definition, criterion)
			continue
		}
		if definition[index].Text != criterion.Text {
			return nil, fmt.Errorf("task/team criterion %q has conflicting text", criterion.ID)
		}
		definition[index].Required = definition[index].Required || criterion.Required
	}
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	return definition, nil
}

// TaskEvidence keeps worker claims separate from independently collected Git metadata.
// Git must be supplied by trusted observation code; it does not verify reported test outcomes.
type TaskEvidence struct {
	Report *ReportRecord   `json:"report"`
	Git    *ReportMetadata `json:"independently_observed_git,omitempty"`
}

func ValidateTaskEvidence(current *Team, task *Task, evidence *TaskEvidence) error {
	if current == nil || task == nil || evidence == nil || evidence.Report == nil {
		return fmt.Errorf("task evidence requires current team, task and structured report")
	}
	record := evidence.Report
	if record.Team != current.Name || !record.CurrentBinding(current) || !validTaskReportIdentity(task, record) {
		return fmt.Errorf("task evidence report identity or binding is stale")
	}
	definition, err := TaskReviewDefinition(current, task)
	if err != nil {
		return err
	}
	if !slices.Equal(record.Definition, definition) {
		return fmt.Errorf("task evidence does not match merged current definition of done")
	}
	if err := record.Reported.Validate(definition); err != nil {
		return err
	}
	if !record.Reported.complete(definition) {
		return fmt.Errorf("task evidence does not report passing required criteria")
	}
	if evidence.Git != nil && evidence.Git.ObservedAt.IsZero() {
		return fmt.Errorf("independent Git evidence requires observation timestamp")
	}
	return nil
}

// ValidateTaskCompletionDefinition verifies stored report requirements without needing mutable team state.
// Integration must also call ValidateTaskEvidence against the current team before accepting completion.
func ValidateTaskCompletionDefinition(task *Task, record *ReportRecord) error {
	if task == nil || !validTaskReportIdentity(task, record) {
		return fmt.Errorf("task completion report identity is invalid")
	}
	if err := record.Definition.Validate(); err != nil {
		return err
	}
	for _, criterion := range taskDefinition(task) {
		if !slices.Contains(record.Definition, criterion) {
			return fmt.Errorf("task completion report omits or changes criterion %q", criterion.ID)
		}
	}
	if err := record.Reported.Validate(record.Definition); err != nil {
		return err
	}
	if !record.Reported.complete(record.Definition) {
		return fmt.Errorf("task completion report requirements did not pass")
	}
	return nil
}
