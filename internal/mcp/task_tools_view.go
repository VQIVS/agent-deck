package mcp

import (
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

type taskSummary struct {
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	State          string               `json:"state"`
	Assignee       string               `json:"assignee,omitempty"`
	Reviewer       string               `json:"reviewer,omitempty"`
	Priority       string               `json:"priority,omitempty"`
	Progress       string               `json:"progress"`
	Dependencies   []string             `json:"dependencies,omitempty"`
	Paths          []string             `json:"paths,omitempty"`
	Description    string               `json:"description,omitempty"`
	Criteria       []team.TaskCriterion `json:"criteria,omitempty"`
	Comments       []team.TaskComment   `json:"comments,omitempty"`
	Category       string               `json:"category,omitempty"`
	ReleaseVersion string               `json:"release_version,omitempty"`
	ReleaseURL     string               `json:"release_url,omitempty"`
}

const recentComments = 5

type taskResult struct {
	Tasks   []taskSummary `json:"tasks"`
	Warning string        `json:"warning,omitempty"`
}

func summarize(task *team.Task, detailed bool) taskSummary {
	done := 0
	for _, criterion := range task.Criteria {
		if criterion.Checked {
			done++
		}
	}
	summary := taskSummary{
		ID: task.ID, Title: task.Title, State: task.State, Assignee: task.Assignee, Reviewer: task.Reviewer,
		Priority: task.Priority, Progress: fmt.Sprintf("%d/%d", done, len(task.Criteria)),
		Dependencies: task.Dependencies, Paths: task.Paths,
		ReleaseVersion: task.ReleaseVersion, ReleaseURL: task.ReleaseURL,
	}
	if detailed {
		summary.Description, summary.Criteria = task.Description, task.Criteria
		summary.Comments = task.Comments[max(0, len(task.Comments)-recentComments):]
	}
	return summary
}

func listTasks(board *team.TaskBoard, name, member string, call *team.WorkflowCall) []taskSummary {
	out := []taskSummary{}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		if name == "my_tasks" {
			if task.Assignee == member || (task.Reviewer == member && board.Category(task.State) == "review") {
				out = append(out, summarize(task, true))
			}
			continue
		}
		if (call.State == "" || task.State == call.State) && (call.Assignee == "" || task.Assignee == call.Assignee) {
			out = append(out, summarize(task, false))
		}
	}
	for i := range out {
		out[i].Category = board.Category(out[i].State)
	}
	return out
}

func changedTasks(board *team.TaskBoard, call *team.WorkflowCall) []taskSummary {
	ids := map[string]bool{call.ID: true}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		if len(task.History) > 0 && task.History[len(task.History)-1].At.Equal(latestChange(board).At) {
			ids[task.ID] = true
		}
	}
	out := []taskSummary{}
	for i := range board.Tasks {
		if ids[board.Tasks[i].ID] {
			summary := summarize(&board.Tasks[i], false)
			summary.Category = board.Category(summary.State)
			out = append(out, summary)
		}
	}
	return out
}

func latestChange(board *team.TaskBoard) (latest team.TaskChange) {
	for i := range board.Tasks {
		for j := range board.Tasks[i].History {
			change := &board.Tasks[i].History[j]
			if change.At.After(latest.At) {
				latest = *change
			}
		}
	}
	return latest
}
