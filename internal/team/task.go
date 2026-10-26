package team

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// TaskCriterion is a stable acceptance item; checked is a progress claim, not verification.
type TaskCriterion struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

const TaskCancelled = "cancelled" //nolint:misspell // Public workflow state specified by the board schema.

// TaskChange records an attributed workflow event.
type TaskChange struct {
	FromCategory string    `json:"from_category,omitempty"`
	ToCategory   string    `json:"to_category,omitempty"`
	Actor        string    `json:"actor"`
	At           time.Time `json:"at"`
	Task         string    `json:"task,omitempty"`
	Field        string    `json:"field"`
	From         string    `json:"from,omitempty"`
	To           string    `json:"to,omitempty"`
}

// TaskComment is an attributed task discussion entry.
type TaskComment struct {
	ID    string    `json:"id"`
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
	Text  string    `json:"text"`
}

// TaskReview records explicit acceptance or requested changes for the current task content.
type TaskReview struct {
	Digest   string    `json:"digest"`
	Reviewer string    `json:"reviewer"`
	Accepted bool      `json:"accepted"`
	Comment  string    `json:"comment"`
	At       time.Time `json:"at"`
}

// TaskActor is resolved by trusted integration; user is the privileged interactive identity.
type TaskActor struct {
	Member     string
	Role       string
	User       bool
	Automation bool
}

// AutomationActor is agent-deck's own policy pass: it may assign and move tasks, never review or approve.
var AutomationActor = TaskActor{Member: "agent-deck", Automation: true}

// ResolveTaskActor derives role from current trusted membership, never tool arguments.
func (t *Team) ResolveTaskActor(name string) (TaskActor, error) {
	for i := range t.Members {
		member := &t.Members[i]
		if member.Name == name {
			if member.Muted {
				return TaskActor{}, fmt.Errorf("muted member cannot mutate tasks")
			}
			return TaskActor{Member: member.Name, Role: member.Role}, nil
		}
	}
	return TaskActor{}, fmt.Errorf("task actor is not a current team member")
}

// TaskMutation supplies explicit authority, time and read-only policy for every mutation.
type TaskMutation struct {
	Actor    TaskActor
	Now      time.Time
	ReadOnly bool
}

func (m TaskMutation) validate() error {
	if m.ReadOnly {
		return fmt.Errorf("read-only: task changes disabled")
	}
	if m.Actor.Automation && m.Actor == AutomationActor && !m.Now.IsZero() {
		return nil
	}
	if m.Now.IsZero() || (!m.Actor.User && (m.Actor.Member == "" ||
		(m.Actor.Role != "master" && m.Actor.Role != "worker"))) {
		return fmt.Errorf("task mutation requires authenticated actor and current time")
	}
	return nil
}

func (m TaskMutation) name() string {
	if m.Actor.User {
		return "user"
	}
	return m.Actor.Member
}

func (m TaskMutation) manages() bool {
	return m.Actor.User || m.Actor.Role == "master" || m.Actor.Automation
}

func validateBoardTask(task *Task, version int) error {
	if err := validateTaskKind(task); err != nil {
		return err
	}
	if version == 1 {
		return validateTask(task)
	}
	if !reportText(task.ID, 200) || !reportText(task.Title, 1000) || len(task.Description) > 64000 {
		return fmt.Errorf("task requires bounded nonempty ID and title")
	}
	if err := task.Paths.Validate(); err != nil {
		return err
	}
	if task.Estimate.Duration < 0 || task.Estimate.Tokens < 0 || invalidCost(task.Estimate.Cost) {
		return fmt.Errorf("task estimate must be finite and nonnegative")
	}
	if taskCategory(task) == "" {
		return fmt.Errorf("unknown workflow state %q", task.State)
	}
	if task.Priority != "" && !slices.Contains([]string{"low", "normal", "high", "urgent"}, task.Priority) {
		return fmt.Errorf("priority must be low, normal, high or urgent")
	}
	if err := validateWorkflowDetails(task); err != nil {
		return err
	}
	return validateTaskTimes(task)
}

func validateWorkflowDetails(task *Task) error {
	if task.Worker != task.Assignee {
		return fmt.Errorf("worker compatibility alias must match assignee")
	}
	category := taskCategory(task)
	if (category == "active" || category == "review") && task.Assignee == "" {
		return fmt.Errorf("active task requires assignee")
	}
	if category == "review" && task.Reviewer == "" {
		return fmt.Errorf("review requires reviewer")
	}
	if category != "todo" && category != TaskCancelled && (len(task.Paths) == 0 || len(task.Criteria) == 0) {
		return fmt.Errorf("planned task requires paths and acceptance criteria")
	}
	return validateTaskCriteria(task)
}

func validateTaskCriteria(task *Task) error {
	ids := map[string]bool{}
	for _, criterion := range task.Criteria {
		if !reportText(criterion.ID, 100) || !reportText(criterion.Text, 2000) || ids[criterion.ID] {
			return fmt.Errorf("acceptance criteria need unique IDs and nonempty text")
		}
		ids[criterion.ID] = true
	}
	if TaskAccepted(task) && !taskCompletionValid(task) {
		return fmt.Errorf("done requires accepted review or passing structured report")
	}
	if TaskShipped(task) && strings.TrimSpace(task.ReleaseVersion) == "" {
		return fmt.Errorf("shipped task requires release version")
	}
	return nil
}

func cloneWorkflowFields(task *Task) {
	if task.Report != nil {
		report := *task.Report
		task.Report = &report
	}
	if task.Measured != nil {
		usage := *task.Measured
		task.Measured = &usage
	}
	task.Criteria = slices.Clone(task.Criteria)
	task.Labels = slices.Clone(task.Labels)
	task.Commits = slices.Clone(task.Commits)
	task.History = slices.Clone(task.History)
	task.Comments = slices.Clone(task.Comments)
	if task.Review != nil {
		review := *task.Review
		task.Review = &review
	}
	if task.Completion != nil {
		data, _ := json.Marshal(task.Completion)
		var record ReportRecord
		_ = json.Unmarshal(data, &record)
		task.Completion = &record
	}
}

func taskCompletionValid(task *Task) bool {
	if task.LegacyCompletion {
		return true
	}
	if acceptedTaskReview(task) {
		return true
	}
	return ValidateTaskCompletionDefinition(task, task.Completion) == nil
}

func acceptedTaskReview(task *Task) bool {
	if task.Review != nil && task.Review.Accepted && task.Review.Reviewer == task.Reviewer && task.Reviewer != "" &&
		!task.Review.At.IsZero() && task.Review.Digest == taskContentDigest(task) {
		return slices.ContainsFunc(task.History, func(change TaskChange) bool {
			return change.Field == "review" && acceptedReviewEvent(&change) &&
				(change.Actor == task.Reviewer || change.Actor == "user") && change.At.Equal(task.Review.At)
		})
	}
	return false
}

func acceptedReviewEvent(change *TaskChange) bool {
	return change.FromCategory == "review" && change.ToCategory == "done" ||
		change.FromCategory == "" && change.ToCategory == "" && change.From == "review" && change.To == "done"
}

func validTaskReportIdentity(task *Task, record *ReportRecord) bool {
	return record != nil && record.Source == "structured" && record.Reported != nil && record.Member == task.Assignee &&
		record.ReportedDone && record.Session != "" && record.Reported.Session == record.Session &&
		record.Reported.Member == record.Member && !record.RecordedAt.IsZero() && record.Reported.TaskID == task.ID
}

func taskContentDigest(task *Task) string {
	data, _ := json.Marshal(struct {
		Title, Description, Assignee, Reviewer, Branch string
		Criteria                                       []TaskCriterion
		Paths                                          Leases
		Dependencies, Commits                          []string
	}{
		task.Title, task.Description, task.Assignee, task.Reviewer, task.Branch,
		task.Criteria, task.Paths, task.Dependencies, task.Commits,
	})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func taskDefinition(task *Task) DefinitionOfDone {
	definition := make(DefinitionOfDone, 0, len(task.Criteria))
	for _, item := range task.Criteria {
		definition = append(definition, DoneCriterion{ID: item.ID, Text: item.Text, Required: true})
	}
	return definition
}

func taskByID(board *TaskBoard, id string) (*Task, error) {
	for i := range board.Tasks {
		if board.Tasks[i].ID == id {
			return &board.Tasks[i], nil
		}
	}
	return nil, fmt.Errorf("unknown task %q", id)
}

func taskEvent(board *TaskBoard, task *Task, mutation TaskMutation, field, from, to string) {
	bindTaskCategory(board, task)
	event := TaskChange{Actor: mutation.name(), At: mutation.Now.UTC(), Task: task.ID, Field: field, From: from, To: to}
	if field == "state" || field == "review" || field == "shipped" {
		event.FromCategory, event.ToCategory = board.Category(from), board.Category(to)
	}
	task.UpdatedAt = event.At
	if taskCategory(task) == "active" && task.StartedAt.IsZero() {
		task.StartedAt = event.At
	}
	if TaskAccepted(task) || taskCategory(task) == TaskCancelled {
		if task.CompletedAt.IsZero() {
			task.CompletedAt = event.At
		}
	} else {
		task.CompletedAt = time.Time{}
	}
	task.History = append(task.History, event)
	board.History = append(board.History, event)
	if len(task.History) > 2000 {
		task.History = slices.Clone(task.History[len(task.History)-2000:])
	}
	if len(board.History) > 10000 {
		board.History = slices.Clone(board.History[len(board.History)-10000:])
	}
}

func validateWorkflowBoard(board *TaskBoard) error {
	data, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("encode workflow board budget: %w", err)
	}
	if len(data) > 7<<20 {
		return fmt.Errorf("workflow board exceeds aggregate 7 MiB budget")
	}
	active := map[string]bool{}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		if TaskActive(task) {
			if active[task.Assignee] {
				return fmt.Errorf("assignee %q has multiple active tasks", task.Assignee)
			}
			active[task.Assignee] = true
		}
		if taskCategory(task) == "ready" || TaskActive(task) {
			for _, id := range task.Dependencies {
				dependency, err := taskByID(board, id)
				if err != nil || !TaskAccepted(dependency) {
					return fmt.Errorf("planned active task has unfinished dependency")
				}
			}
		}
	}
	return nil
}

func boundedTaskText(text string) bool { return strings.TrimSpace(text) != "" && len(text) <= 16000 }
