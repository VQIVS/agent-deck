package ui

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/team"
)

// KanbanScreen contains transient navigation only; task data stays on the team board.
type KanbanScreen struct {
	Workflow     team.WorkflowDefinition
	MoveIndex    int
	MoveStages   []team.WorkflowStage
	Selected     string
	Column       string
	Detail       bool
	DetailOffset int
	Filter       KanbanFilter
	Swimlanes    bool
	Move         bool
	moveTaskID   string
	searching    bool
	lastClickID  string
	lastClickAt  time.Time
}

// KanbanFilter restricts cards using conjunctive assignee, label, state and text filters.
type KanbanFilter struct{ Assignee, Label, State, Query string }

// KanbanOptions supplies explicit rendering time and member badge colors.
type KanbanOptions struct {
	Workflow      team.WorkflowDefinition
	GitFacts      map[string]*taskGitResult
	GitTargets    map[string]TaskEvidenceTarget
	Width, Height int
	Now           time.Time
	Theme         Theme
	ReadOnly      bool
	MemberColors  map[string]lipgloss.Color
	Definitions   map[string]team.DefinitionOfDone
}

func optsWorkflowCategory(opts *KanbanOptions, stage string) string {
	return opts.Workflow.Category(stage)
}

// KanbanIntent requests guarded integration; it never executes task mutations.
type KanbanIntent struct{ Action, TaskID, State, Assignee string }

// KanbanTarget binds a visible mouse rectangle to its original task/action identity.
type KanbanTarget struct {
	Rect
	TaskID, Action, State string
}

func workflowStageNames(definition team.WorkflowDefinition) []string {
	stages := definition.Resolved().Stages
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, stage.Name)
	}
	return names
}

func (s *KanbanScreen) states() []string { return workflowStageNames(s.Workflow) }

func (s *KanbanScreen) moveStates() []string {
	if s.MoveStages == nil {
		return s.states()
	}
	return workflowStageNames(team.WorkflowDefinition{Stages: s.MoveStages})
}

func kanbanAssignee(task *team.Task) string {
	return task.Assignee
}

func kanbanMatches(task *team.Task, filter KanbanFilter) bool {
	if filter.Assignee != "" && kanbanAssignee(task) != filter.Assignee {
		return false
	}
	if filter.Label != "" && !slices.Contains(task.Labels, filter.Label) {
		return false
	}
	if filter.State != "" && task.State != filter.State {
		return false
	}
	text := strings.ToLower(task.ID + " " + task.Title + " " + task.Description + " " + strings.Join(task.Labels, " "))
	return strings.Contains(text, strings.ToLower(strings.TrimSpace(filter.Query)))
}

func kanbanTasks(tasks []team.Task, filter KanbanFilter, state string, swimlanes bool) []*team.Task {
	result := make([]*team.Task, 0, len(tasks))
	for i := range tasks {
		if kanbanMatches(&tasks[i], filter) && (state == "" || tasks[i].State == state) {
			result = append(result, &tasks[i])
		}
	}
	if swimlanes {
		sort.SliceStable(result, func(i, j int) bool { return kanbanAssignee(result[i]) < kanbanAssignee(result[j]) })
	}
	return result
}

func (s *KanbanScreen) reconcile(tasks []team.Task) *team.Task {
	visible := kanbanTasks(tasks, s.Filter, "", s.Swimlanes)
	for _, task := range visible {
		if task.ID == s.Selected {
			s.Column = task.State
			return task
		}
	}
	if !slices.Contains(s.states(), s.Column) {
		s.Column = s.states()[0]
	}
	for _, task := range visible {
		if task.State == s.Column {
			s.Selected = task.ID
			return task
		}
	}
	s.Selected = ""
	return nil
}

func kanbanTask(tasks []team.Task, id string) *team.Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}
