package ui

import (
	"context"
	"fmt"
	"slices"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	"github.com/vqivs/agent-deck/internal/team"
)

// TaskEvidenceTarget captures the task binding used for a read-only Git observation.
type TaskEvidenceTarget struct {
	TaskID    string
	Directory string
	Base      string
	Paths     []string
}

// TaskGitObservation preserves provenance and errors without changing task-reported Git links.
type TaskGitObservation struct {
	Target TaskEvidenceTarget
	Facts  gitinfo.TaskFacts
	Err    error
}

func NewTaskEvidenceTarget(task *team.Task, directory, base string) TaskEvidenceTarget {
	if task == nil {
		return TaskEvidenceTarget{}
	}
	return TaskEvidenceTarget{TaskID: task.ID, Directory: directory, Base: base, Paths: slices.Clone(task.Paths)}
}

// CurrentTaskEvidenceTarget resolves the current code checkout, never a session control directory.
func CurrentTaskEvidenceTarget(current *team.Team, task *team.Task) TaskEvidenceTarget {
	if current == nil || task == nil {
		return TaskEvidenceTarget{}
	}
	directory := current.Repo
	for i := range current.Members {
		member := &current.Members[i]
		if member.Name == task.Assignee {
			directory = member.Dir
			break
		}
	}
	return NewTaskEvidenceTarget(task, directory, current.Base)
}

func ObserveTaskEvidence(ctx context.Context, target *TaskEvidenceTarget) TaskGitObservation {
	if target == nil {
		return TaskGitObservation{Err: fmt.Errorf("task evidence needs a current target")}
	}
	captured := *target
	captured.Paths = slices.Clone(target.Paths)
	facts, err := gitinfo.ObserveTask(ctx, captured.Directory, captured.Base, captured.Paths)
	return TaskGitObservation{Target: captured, Facts: facts, Err: err}
}
