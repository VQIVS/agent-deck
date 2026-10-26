package team

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// WorkflowStage separates a persisted stage name from its execution category.
type WorkflowStage struct {
	Name     string `json:"name" yaml:"name"`
	Color    string `json:"color" yaml:"color"`
	Category string `json:"category" yaml:"category"`
}

// WorkflowTransition constrains entry into a named stage without granting evidence authority.
type WorkflowTransition struct {
	From []string `json:"from,omitempty" yaml:"from,omitempty"`
	By   []string `json:"by,omitempty" yaml:"by,omitempty"`
}

// WorkflowDefinition is an ordered team workflow; an empty definition uses the compatibility default.
type WorkflowDefinition struct {
	Stages      []WorkflowStage               `json:"stages,omitempty" yaml:"stages,omitempty"`
	Transitions map[string]WorkflowTransition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
}

func DefaultWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Stages: []WorkflowStage{
			{"backlog", "#929BAA", "todo"},
			{"ready", "#B7A3F5", "ready"},
			{"in_progress", "#E5BB79", "active"},
			{"review", "#DDA2CD", "review"},
			{"done", "#89C9A0", "done"},
			{"released", "#89C9A0", "shipped"},
			{"blocked", "#EF9393", "blocked"},
			{TaskCancelled, "#929BAA", TaskCancelled},
		},
		Transitions: map[string]WorkflowTransition{
			"released": {From: []string{"done"}, By: []string{"release-manager", "user"}},
		},
	}
}

func (w WorkflowDefinition) Resolved() WorkflowDefinition {
	if len(w.Stages) == 0 && len(w.Transitions) == 0 {
		return DefaultWorkflow()
	}
	return w
}

func (w WorkflowDefinition) Clone() WorkflowDefinition {
	cloned := WorkflowDefinition{Stages: slices.Clone(w.Stages)}
	if w.Transitions != nil {
		cloned.Transitions = make(map[string]WorkflowTransition, len(w.Transitions))
		for name, rule := range w.Transitions {
			cloned.Transitions[name] = WorkflowTransition{From: slices.Clone(rule.From), By: slices.Clone(rule.By)}
		}
	}
	return cloned
}

func (w WorkflowDefinition) Category(stage string) string {
	for _, item := range w.Resolved().Stages {
		if item.Name == stage {
			return item.Category
		}
	}
	return ""
}

func (w WorkflowDefinition) Destination(category string) string {
	for _, item := range w.Resolved().Stages {
		if item.Category == category {
			return item.Name
		}
	}
	return ""
}

func (w WorkflowDefinition) Validate() error {
	w = w.Resolved()
	if len(w.Stages) == 0 || len(w.Stages) > 64 {
		return fmt.Errorf("workflow needs 1 to 64 ordered stages")
	}
	names := map[string]bool{}
	color := regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	categories := []string{"todo", "ready", "active", "review", "done", "shipped", "blocked", TaskCancelled}
	for _, stage := range w.Stages {
		if !reportText(stage.Name, 100) || strings.TrimSpace(stage.Name) != stage.Name || names[stage.Name] {
			return fmt.Errorf("workflow stage names must be bounded, nonempty and unique: %q", stage.Name)
		}
		if !slices.Contains(categories, stage.Category) || !color.MatchString(stage.Color) {
			return fmt.Errorf("workflow stage %q needs a valid category and #RRGGBB color", stage.Name)
		}
		names[stage.Name] = true
	}
	for _, category := range categories {
		if w.Destination(category) == "" {
			return fmt.Errorf("workflow needs category %q", category)
		}
	}
	return w.validateTransitions(names)
}

func (w WorkflowDefinition) validateTransitions(names map[string]bool) error {
	for target, rule := range w.Transitions {
		if !names[target] || len(rule.From) == 0 || len(rule.By) == 0 {
			return fmt.Errorf("workflow transition %q needs known target, sources and actors", target)
		}
		for _, from := range rule.From {
			if !names[from] {
				return fmt.Errorf("workflow transition %q has unknown source %q", target, from)
			}
		}
		for _, actor := range rule.By {
			if !reportText(actor, 200) {
				return fmt.Errorf("workflow transition %q has invalid actor", target)
			}
		}
	}
	return nil
}

func (b *TaskBoard) Category(stage string) string       { return b.Workflow.Category(stage) }
func (b *TaskBoard) Destination(category string) string { return b.Workflow.Destination(category) }

func taskCategory(task *Task) string {
	if task.category != "" && task.categoryState == task.State {
		return task.category
	}
	return DefaultWorkflow().Category(task.State)
}

func TaskAccepted(task *Task) bool {
	return taskCategory(task) == "done" || taskCategory(task) == "shipped"
}
func TaskShipped(task *Task) bool { return taskCategory(task) == "shipped" }

func bindTaskCategory(board *TaskBoard, task *Task) {
	task.category, task.categoryState = board.Category(task.State), task.State
}

// SetWorkflow rejects occupied stage removal or category changes before replacing the definition.
func (b *TaskBoard) SetWorkflow(definition WorkflowDefinition, mutation TaskMutation) error {
	if err := mutation.validate(); err != nil {
		return err
	}
	if !mutation.manages() {
		return fmt.Errorf("workflow configuration requires master or user")
	}
	return b.bindWorkflow(definition)
}

func (b *TaskBoard) bindWorkflow(definition WorkflowDefinition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	for i := range b.Tasks {
		task := &b.Tasks[i]
		if definition.Category(task.State) != b.Category(task.State) {
			return fmt.Errorf("occupied workflow stage %q cannot be removed or change category", task.State)
		}
	}
	candidate := cloneBoard(b)
	candidate.Workflow = definition.Resolved().Clone()
	if err := candidate.Validate(); err != nil {
		return err
	}
	*b = candidate
	return nil
}
