package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/team"
)

// KanbanField is a task form text value; multiline input uses Ctrl+J.
type KanbanField struct{ Label, Value string }

// KanbanForm retains task identity and revision until explicit save.
type KanbanForm struct {
	Open           bool
	Action         string
	Revision       uint64
	Original       team.Task
	Fields         []KanbanField
	Index          int
	Error          string
	OriginalDigest string
	TargetStage    string
	Workflow       team.WorkflowDefinition
}

func NewKanbanForm(action string, task *team.Task, revision uint64) *KanbanForm {
	f := &KanbanForm{Open: true, Action: action, Revision: revision}
	if task != nil {
		f.Original = *task
		f.OriginalDigest, _ = kanbanEditBaseline(&team.TaskBoard{Tasks: []team.Task{*task}},
			&KanbanEdit{Intent: KanbanIntent{TaskID: task.ID}})
	}
	t := &f.Original
	if action == "release" {
		f.Fields = []KanbanField{{Label: "Release version"}, {Label: "Release URL (optional)"}}
		return f
	}
	if action == "review" {
		f.Fields = []KanbanField{{Label: "Decision (accept/changes)"}, {Label: "Review comment"}}
		return f
	}
	if action == "comment" {
		f.Fields = []KanbanField{{Label: "Comment"}}
		return f
	}
	if action == "assign" {
		f.Fields = []KanbanField{{Label: "Assignee", Value: kanbanAssignee(t)}}
		return f
	}
	criteria := make([]string, 0, len(t.Criteria))
	for _, c := range t.Criteria {
		criteria = append(criteria, c.ID+": "+c.Text)
	}
	duration := ""
	if t.Estimate.Duration > 0 {
		duration = t.Estimate.Duration.String()
	}
	f.Fields = []KanbanField{
		{"ID", t.ID},
		{"Title", t.Title},
		{"Description", t.Description},
		{"Criteria (id: text per line)", strings.Join(criteria, "\n")},
		{"Paths", strings.Join(t.Paths, ", ")},
		{"Dependencies", strings.Join(t.Dependencies, ", ")},
		{"Priority", t.Priority},
		{"Labels", strings.Join(t.Labels, ", ")},
		{"Assignee", kanbanAssignee(t)},
		{"Reviewer", t.Reviewer},
		{"Estimate", duration},
	}
	if action == "edit" {
		f.Index = 1
	}
	return f
}

func (f *KanbanForm) Build() (*KanbanEdit, error) {
	values := map[string]string{}
	for _, field := range f.Fields {
		values[field.Label] = strings.TrimSpace(field.Value)
	}
	edit := &KanbanEdit{Intent: KanbanIntent{Action: f.Action, TaskID: f.Original.ID}, Revision: f.Revision}
	edit.Original = f.OriginalDigest
	if f.Action == "release" {
		return f.buildRelease(edit, values)
	}
	if f.Action == "review" {
		edit.Intent.State = values["Decision (accept/changes)"]
		edit.Comment = values["Review comment"]
		return edit, validateReviewForm(f.Workflow.Category(f.Original.State), edit)
	}
	if f.Action == "comment" {
		edit.Comment = values["Comment"]
		return edit, nil
	}
	if f.Action == "assign" {
		edit.Intent.Assignee = values["Assignee"]
		return edit, nil
	}
	task := f.Original
	task.ID, task.Title, task.Description = values["ID"], values["Title"], values["Description"]
	if f.Action == "edit" && task.ID != f.Original.ID {
		return nil, fmt.Errorf("task ID cannot change")
	}
	if task.ID == "" || task.Title == "" {
		return nil, fmt.Errorf("task ID and title are required")
	}
	task.Paths = team.Leases(kanbanCSV(values["Paths"]))
	task.Dependencies, task.Labels = kanbanCSV(values["Dependencies"]), kanbanCSV(values["Labels"])
	task.Priority, task.Assignee, task.Reviewer = values["Priority"], values["Assignee"], values["Reviewer"]
	criteria, err := kanbanFormCriteria(values["Criteria (id: text per line)"], f.Original.Criteria)
	if err != nil {
		return nil, err
	}
	task.Criteria = criteria
	task.Estimate.Duration = 0
	if value := values["Estimate"]; value != "" {
		task.Estimate.Duration, err = time.ParseDuration(value)
		if err != nil || task.Estimate.Duration < 0 {
			return nil, fmt.Errorf("estimate needs nonnegative duration such as 10m")
		}
	}
	if f.Action == "create" {
		task.State = f.Workflow.Destination("todo")
		task.Estimate.Parallel = true
	}
	edit.Task = &task
	return edit, nil
}

func (f *KanbanForm) buildRelease(edit *KanbanEdit, values map[string]string) (*KanbanEdit, error) {
	edit.Intent.Action, edit.Intent.State = "move", f.TargetStage
	edit.Release = &team.TaskRelease{Version: values["Release version"], URL: values["Release URL (optional)"]}
	if edit.Release.Version == "" {
		return nil, fmt.Errorf("release version is required")
	}
	return edit, nil
}

func validateReviewForm(state string, edit *KanbanEdit) error {
	if state != "review" {
		return fmt.Errorf("only tasks in review can receive a review decision")
	}
	if edit.Intent.State != "accept" && edit.Intent.State != "changes" {
		return fmt.Errorf("review decision must be accept or changes")
	}
	if edit.Comment == "" || len(edit.Comment) > 16000 {
		return fmt.Errorf("review requires a comment of 1 to 16000 bytes")
	}
	return nil
}

func kanbanCSV(value string) []string {
	var result []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" && !slices.Contains(result, part) {
			result = append(result, part)
		}
	}
	return result
}

func kanbanFormCriteria(text string, previous []team.TaskCriterion) ([]team.TaskCriterion, error) {
	var result []team.TaskCriterion
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		id, value, ok := strings.Cut(line, ":")
		id, value = strings.TrimSpace(id), strings.TrimSpace(value)
		if !ok || id == "" || value == "" {
			return nil, fmt.Errorf("criteria use id: text, one per line")
		}
		criterion := team.TaskCriterion{ID: id, Text: value}
		for _, old := range previous {
			if old.ID == id && old.Text == value {
				criterion.Checked = old.Checked
			}
		}
		result = append(result, criterion)
	}
	return result, nil
}

func KanbanFormKey(form *KanbanForm, key tea.KeyMsg, readOnly bool) *KanbanEdit {
	switch key.String() {
	case "esc":
		form.Open = false
	case "tab":
		form.Index = (form.Index + 1) % len(form.Fields)
	case "shift+tab":
		form.Index = (form.Index + len(form.Fields) - 1) % len(form.Fields)
	case "ctrl+s":
		if readOnly {
			form.Error = "Read-only: task save disabled"
			return nil
		}
		edit, err := form.Build()
		if err != nil {
			form.Error = err.Error()
			return nil
		}
		return edit
	case "ctrl+j":
		form.Fields[form.Index].Value += "\n"
	default:
		if !readOnly && (form.Action != "edit" || form.Index != 0) {
			editInboxText(&form.Fields[form.Index].Value, key)
		}
	}
	return nil
}
