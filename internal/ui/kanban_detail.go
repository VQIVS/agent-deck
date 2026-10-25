package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func renderKanbanDetail(task *team.Task, screen *KanbanScreen, opts *KanbanOptions) (string, []KanbanTarget) {
	return renderKanbanDetailRequirements(task, screen, opts,
		TaskDefinitionDetail(opts.Definitions[task.ID], max(1, min(110, opts.Width-4))))
}

func renderKanbanDetailRequirements(task *team.Task, screen *KanbanScreen,
	opts *KanbanOptions, requirements string,
) (string, []KanbanTarget) {
	lines := kanbanDetailLines(task)
	lines = append(lines, "Stage category: "+opts.Workflow.Category(task.State))
	if task.ReleaseVersion != "" {
		lines = append(lines, "Recorded release version: "+task.ReleaseVersion, "Release URL: "+task.ReleaseURL,
			"Shipped at: "+task.ShippedAt.UTC().Format("2006-01-02 15:04"))
	}
	if requirements != "" {
		lines = append(lines, "", requirements)
	}
	if opts.GitFacts != nil {
		target := opts.GitTargets[task.ID]
		lines = append(lines, CurrentTaskGitLines(opts.GitFacts[task.ID], &target)...)
	}
	width := max(1, min(110, opts.Width-4))
	wrapped := strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")
	height := max(0, opts.Height-6)
	screen.DetailOffset = max(0, min(screen.DetailOffset, max(0, len(wrapped)-height)))
	end := min(len(wrapped), screen.DetailOffset+height)
	header := kanbanFit(inboxLine("Task "+task.ID+" · "+task.State+" · "+task.Title), width)
	footer := kanbanFit("Esc board · PgUp/PgDown scroll · e edit · c comment · a assign · m move · w worker", width)
	view := header + "\n" + strings.Join(wrapped[screen.DetailOffset:end], "\n") + "\n" + footer
	panel := lipgloss.NewStyle().Width(width).Border(lipgloss.RoundedBorder()).
		BorderForeground(opts.Theme.Accent).Background(opts.Theme.Surface).Render(view)
	panel = lipgloss.Place(opts.Width, opts.Height, lipgloss.Center, lipgloss.Center, panel)
	return kanbanBound(panel, opts.Width, opts.Height), nil
}

func kanbanDetailLines(task *team.Task) []string {
	lines := []string{
		"Assignee: " + kanbanOwner(kanbanAssignee(task)) + " · Reviewer: " + kanbanOwner(task.Reviewer),
		"Priority: " + task.Priority + " · Labels: " + strings.Join(task.Labels, ", "),
		"Paths: " + strings.Join(task.Paths, ", "), "Dependencies: " + strings.Join(task.Dependencies, ", "),
		"", "Description", task.Description, "", "Acceptance criteria (claimed progress)",
	}
	for _, criterion := range task.Criteria {
		mark := " "
		if criterion.Checked {
			mark = "x"
		}
		lines = append(lines, "["+mark+"] "+criterion.ID+": "+criterion.Text)
	}
	lines = append(lines, "", "Git links", "Branch: "+task.Branch)
	for _, commit := range task.Commits {
		lines = append(lines, "Commit: "+commit)
	}
	lines = append(lines, "Worker pane: "+kanbanOwner(kanbanAssignee(task)), "", "Report")
	if task.Report != nil {
		lines = append(lines, task.Report.Text, "Reported tests: "+task.Report.Tests)
	}
	if task.Completion != nil {
		lines = append(lines, "Completion source: "+task.Completion.Source)
		if task.Completion.Reported != nil {
			lines = append(lines, task.Completion.Reported.Summary)
		}
	}
	if task.Review != nil {
		lines = append(lines, fmt.Sprintf("Review: %s · accepted %t · %s",
			task.Review.Reviewer, task.Review.Accepted, task.Review.Comment))
	}
	lines = append(lines, "", "History")
	for i := range task.History {
		change := &task.History[i]
		lines = append(lines, change.At.UTC().Format("2006-01-02 15:04")+" "+change.Actor+
			" · "+change.Field+": "+change.From+" → "+change.To)
	}
	lines = append(lines, "", "Comments")
	for _, comment := range task.Comments {
		lines = append(lines, comment.At.UTC().Format("2006-01-02 15:04")+" "+comment.Actor+": "+comment.Text)
	}
	for i := range lines {
		lines[i] = inboxLine(lines[i])
	}
	return lines
}
