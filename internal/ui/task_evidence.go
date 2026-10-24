package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

func (o *TaskGitObservation) Matches(target *TaskEvidenceTarget) bool {
	return o != nil && target != nil && o.Target.TaskID == target.TaskID && o.Target.Directory == target.Directory &&
		o.Target.Base == target.Base && slices.Equal(o.Target.Paths, target.Paths)
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

// TaskEvidenceDetail displays claims and observations separately; Git never verifies reported tests.
func TaskEvidenceDetail(task *team.Task, observation *TaskGitObservation,
	target *TaskEvidenceTarget, width int,
) string {
	if task == nil || width <= 0 {
		return ""
	}
	lines := []string{"Worker-reported task links (claims)", "Branch reported: " + task.Branch}
	for _, commit := range task.Commits {
		lines = append(lines, "Commit reported: "+commit)
	}
	if task.Report != nil {
		lines = append(lines, "Summary reported: "+task.Report.Text, "Tests reported: "+task.Report.Tests)
	}
	lines = append(lines, "", "Independently observed Git")
	switch {
	case target == nil || target.TaskID != task.ID || !slices.Equal(target.Paths, task.Paths) ||
		!observation.Matches(target):
		lines = append(lines, "Unavailable: no observation for current task/checkout/paths")
	case observation.Err != nil:
		lines = append(lines, "Unavailable: "+observation.Err.Error())
	case observation.Facts.ObservedAt.IsZero():
		lines = append(lines, "Unavailable: missing observation timestamp")
	default:
		facts := &observation.Facts
		lines = append(lines, "Observed at: "+facts.ObservedAt.UTC().Format("2006-01-02 15:04:05Z"),
			fmt.Sprintf("Branch: %s · ahead %d · base %s", facts.Branch, facts.Ahead, facts.Base),
			"Commits touching task paths:")
		for _, commit := range facts.Commits {
			lines = append(lines, commit.Hash+" "+commit.Subject)
		}
		if facts.Truncated {
			lines = append(lines, "Additional matching commits omitted")
		}
	}
	lines = append(lines, "Git observations do not verify test outcomes or acceptance criteria.")
	for i := range lines {
		lines[i] = RenderInboxLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
