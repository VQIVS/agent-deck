package ui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/team"
)

func KanbanKey(tasks []team.Task, screen *KanbanScreen, key tea.KeyMsg, readOnly bool) *KanbanIntent {
	if screen.Move {
		if kanbanTask(tasks, screen.moveTaskID) == nil {
			screen.Move = false
			return nil
		}
		return kanbanMoveKey(screen, key, readOnly)
	}
	task := screen.reconcile(tasks)
	if screen.searching {
		if key.String() == "enter" || key.String() == "esc" {
			screen.searching = false
		} else {
			editInboxText(&screen.Filter.Query, key)
		}
		screen.reconcile(tasks)
		return nil
	}
	return kanbanNormalKey(tasks, screen, task, key.String(), readOnly)
}

func kanbanNormalKey(tasks []team.Task, screen *KanbanScreen, task *team.Task,
	key string, readOnly bool,
) *KanbanIntent {
	if kanbanNavigate(tasks, screen, key) {
		return nil
	}
	if kanbanFilterKey(tasks, screen, key) {
		return nil
	}
	switch key {
	case "esc":
		if screen.Detail {
			screen.Detail = false
			return nil
		}
		return &KanbanIntent{Action: "close"}
	case "enter":
		screen.Detail = task != nil
		screen.DetailOffset = 0
	case "/":
		screen.searching = true
	case "s":
		screen.Swimlanes = !screen.Swimlanes
	case "m":
		if task != nil && !readOnly {
			screen.Move = true
			screen.moveTaskID = task.ID
		}
	case "n":
		if !readOnly {
			return &KanbanIntent{Action: "create"}
		}
	case "e", "c", "a", "w":
		return kanbanTaskIntent(task, key, readOnly)
	case "r":
		return &KanbanIntent{Action: "refresh"}
	}
	return nil
}

func kanbanFilterKey(tasks []team.Task, screen *KanbanScreen, key string) bool {
	switch key {
	case "f":
		screen.Filter.Assignee = kanbanCycleFilter(tasks, "assignee", screen.Filter.Assignee)
	case "t":
		screen.Filter.Label = kanbanCycleFilter(tasks, "label", screen.Filter.Label)
	case "z":
		screen.Filter.State = kanbanNextValue(KanbanStates(), screen.Filter.State)
	case "0":
		screen.Filter = KanbanFilter{}
	default:
		return false
	}
	return true
}

func kanbanCycleFilter(tasks []team.Task, kind, current string) string {
	values := []string{}
	for i := range tasks {
		if kind == "assignee" {
			values = append(values, kanbanAssignee(&tasks[i]))
		} else {
			values = append(values, tasks[i].Labels...)
		}
	}
	slices.Sort(values)
	return kanbanNextValue(slices.Compact(values), current)
}

func kanbanNextValue(values []string, current string) string {
	index := slices.Index(values, current)
	for i := index + 1; i < len(values); i++ {
		if values[i] != "" {
			return values[i]
		}
	}
	return ""
}

func kanbanNavigate(tasks []team.Task, screen *KanbanScreen, key string) bool {
	switch key {
	case "left", "h":
		screen.horizontal(tasks, -1)
	case "right", "l":
		screen.horizontal(tasks, 1)
	case "up", "k":
		if screen.Detail {
			screen.DetailOffset = max(0, screen.DetailOffset-1)
		} else {
			screen.vertical(tasks, -1)
		}
	case "down", "j":
		if screen.Detail {
			screen.DetailOffset++
		} else {
			screen.vertical(tasks, 1)
		}
	case "pgup":
		screen.DetailOffset = max(0, screen.DetailOffset-8)
	case "pgdown":
		screen.DetailOffset += 8
	default:
		return false
	}
	return true
}

func kanbanTaskIntent(task *team.Task, key string, readOnly bool) *KanbanIntent {
	if task == nil || (readOnly && key != "w") {
		return nil
	}
	actions := map[string]string{"e": "edit", "c": "comment", "a": "assign", "w": "worker"}
	return &KanbanIntent{Action: actions[key], TaskID: task.ID, Assignee: kanbanAssignee(task)}
}

func kanbanMoveKey(screen *KanbanScreen, key tea.KeyMsg, readOnly bool) *KanbanIntent {
	screen.Move = false
	if readOnly {
		return nil
	}
	if len(key.Runes) == 1 && key.Runes[0] >= '1' && key.Runes[0] <= '7' {
		return &KanbanIntent{Action: "move", TaskID: screen.moveTaskID, State: KanbanStates()[int(key.Runes[0]-'1')]}
	}
	return nil
}

func (s *KanbanScreen) horizontal(tasks []team.Task, delta int) {
	states := KanbanStates()
	index := max(0, slices.Index(states, s.Column))
	s.Column = states[max(0, min(len(states)-1, index+delta))]
	s.Selected = ""
	items := kanbanTasks(tasks, s.Filter, s.Column, s.Swimlanes)
	if len(items) > 0 {
		s.Selected = items[0].ID
	}
	s.DetailOffset = 0
}

func (s *KanbanScreen) vertical(tasks []team.Task, delta int) {
	items := kanbanTasks(tasks, s.Filter, s.Column, s.Swimlanes)
	for i, task := range items {
		if task.ID == s.Selected {
			s.Selected = items[max(0, min(len(items)-1, i+delta))].ID
			break
		}
	}
	s.DetailOffset = 0
}

func KanbanClick(tasks []team.Task, screen *KanbanScreen, target *KanbanTarget, readOnly bool) *KanbanIntent {
	task := kanbanTask(tasks, target.TaskID)
	if task == nil || !kanbanMatches(task, screen.Filter) || (target.State != "" && task.State != target.State) {
		return nil
	}
	screen.Selected, screen.Column = task.ID, task.State
	switch target.Action {
	case "select":
	case "detail":
		screen.Detail = true
		screen.DetailOffset = 0
	case "worker":
		return kanbanTaskIntent(task, "w", readOnly)
	case "edit":
		return kanbanTaskIntent(task, "e", readOnly)
	case "previous", "next":
		if readOnly {
			return nil
		}
		step := 1
		if target.Action == "previous" {
			step = -1
		}
		states := KanbanStates()
		index := slices.Index(states, task.State) + step
		if index >= 0 && index < len(states) {
			return &KanbanIntent{Action: "move", TaskID: task.ID, State: states[index]}
		}
	}
	return nil
}

// KanbanClickAt uses caller-supplied time; two clicks on the same card within 400ms open detail.
func KanbanClickAt(tasks []team.Task, screen *KanbanScreen, target *KanbanTarget,
	readOnly bool, now time.Time,
) *KanbanIntent {
	if target.Action != "select" {
		return KanbanClick(tasks, screen, target, readOnly)
	}
	clicked := *target
	double := screen.lastClickID == target.TaskID && !now.IsZero() &&
		!now.Before(screen.lastClickAt) && now.Sub(screen.lastClickAt) <= 400*time.Millisecond
	if double {
		clicked.Action = "detail"
	}
	result := KanbanClick(tasks, screen, &clicked, readOnly)
	screen.lastClickID, screen.lastClickAt = target.TaskID, now
	return result
}
