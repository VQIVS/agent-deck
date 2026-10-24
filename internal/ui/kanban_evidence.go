package ui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	"github.com/vqivs/agent-deck/internal/team"
)

type taskGitResult struct {
	TaskID string
	Facts  gitinfo.TaskFacts
	Err    error
	Target TaskEvidenceTarget
}

func (m *Model) taskEvidenceTargets(current *team.Team) map[string]TaskEvidenceTarget {
	targets := make(map[string]TaskEvidenceTarget, len(m.board.State.Board.Tasks))
	for i := range m.board.State.Board.Tasks {
		task := &m.board.State.Board.Tasks[i]
		targets[task.ID] = CurrentTaskEvidenceTarget(current, task)
	}
	return targets
}

func (m *Model) readTaskGit() tea.Cmd {
	if m.opts.NoGit || m.teams == nil || m.teams.Manager == nil {
		return nil
	}
	task := kanbanTask(m.board.State.Board.Tasks, m.board.Kanban.Selected)
	if task == nil || len(task.Paths) == 0 {
		return nil
	}
	target := CurrentTaskEvidenceTarget(&m.teams.Manager.Team, task)
	ctx := m.context()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		observation := ObserveTaskEvidence(ctx, &target)
		return taskGitResult{
			TaskID: target.TaskID, Facts: observation.Facts, Err: observation.Err, Target: target,
		}
	}
}

// CurrentTaskGitResult rejects asynchronous results captured for an obsolete checkout, base or path set.
func CurrentTaskGitResult(result *taskGitResult, target *TaskEvidenceTarget) bool {
	return result != nil && target != nil && result.TaskID == target.TaskID &&
		result.Target.TaskID == target.TaskID && result.Target.Directory == target.Directory &&
		result.Target.Base == target.Base && slices.Equal(result.Target.Paths, target.Paths)
}

func (m *Model) applyTaskGitResult(result *taskGitResult) {
	if result == nil || m.teams == nil || m.teams.Manager == nil {
		return
	}
	task := kanbanTask(m.board.State.Board.Tasks, result.TaskID)
	if task == nil {
		return
	}
	target := CurrentTaskEvidenceTarget(&m.teams.Manager.Team, task)
	if !CurrentTaskGitResult(result, &target) {
		return
	}
	if m.board.Git == nil {
		m.board.Git = make(map[string]*taskGitResult)
	}
	captured := *result
	captured.Target.Paths = slices.Clone(result.Target.Paths)
	captured.Facts.Commits = slices.Clone(result.Facts.Commits)
	m.board.Git[result.TaskID] = &captured
}

// CurrentTaskGitLines validates captured provenance again at render time.
func CurrentTaskGitLines(result *taskGitResult, target *TaskEvidenceTarget) []string {
	if !CurrentTaskGitResult(result, target) {
		return []string{"Independent Git observation: unavailable for current task/checkout/base/paths"}
	}
	return taskGitLines(result)
}

func taskGitLines(result *taskGitResult) []string {
	if result == nil {
		return []string{"Independent Git observation: unavailable"}
	}
	if result.Err != nil {
		return []string{"Independent Git observation unavailable: " + result.Err.Error()}
	}
	facts := &result.Facts
	if facts.ObservedAt.IsZero() {
		return []string{"Independent Git observation unavailable: missing observation timestamp"}
	}
	lines := []string{fmt.Sprintf("Independently observed Git: branch=%s ahead=%d at=%s",
		facts.Branch, facts.Ahead, facts.ObservedAt.UTC().Format(time.RFC3339))}
	for _, commit := range facts.Commits {
		lines = append(lines, "Observed commit: "+commit.Hash+" "+commit.Subject)
	}
	if facts.Truncated {
		lines = append(lines, "Observed commits truncated to 100")
	}
	return lines
}
