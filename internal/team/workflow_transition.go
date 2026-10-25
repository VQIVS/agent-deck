package team

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// TaskRelease supplies explicit shipment metadata for a stage move.
type TaskRelease struct {
	Version string `json:"version"`
	URL     string `json:"url,omitempty"`
}

func workerCategoryMove(from, to string) bool {
	return from == "active" && (to == "active" || to == "review" || to == "blocked") ||
		from == "blocked" && to == "active"
}

func validCategoryMove(from, to string) bool {
	if from == "" || to == "" || from == "shipped" {
		return false
	}
	if from == to {
		return true
	}
	if to == TaskCancelled {
		return from != "done" && from != TaskCancelled
	}
	moves := map[string][]string{
		"todo": {"ready", "blocked"}, "ready": {"todo", "active", "blocked"},
		"active": {"review", "blocked", "done"}, "review": {"active", "blocked", "done"},
		"blocked": {"todo", "ready", "active"}, TaskCancelled: {"todo"}, "done": {"shipped"},
	}
	return slices.Contains(moves[from], to)
}

func (b *TaskBoard) authorizeTransition(from, to string, mutation TaskMutation) error {
	if b.Category(from) == "shipped" {
		return fmt.Errorf("shipped is terminal")
	}
	rule, exists := b.Workflow.Resolved().Transitions[to]
	if !exists {
		return nil
	}
	if !slices.Contains(rule.From, from) {
		return fmt.Errorf("transition to %q disallows source %q", to, from)
	}
	actor := mutation.Actor.Member
	if mutation.Actor.User {
		actor = "user"
	}
	if !slices.Contains(rule.By, actor) {
		return fmt.Errorf("actor %q cannot enter workflow stage %q", actor, to)
	}
	return nil
}

func (b *TaskBoard) shipTask(task *Task, stage string, release *TaskRelease, mutation TaskMutation) error {
	if taskCategory(task) != "done" || !taskCompletionValid(task) {
		return fmt.Errorf("shipment requires previously accepted done work")
	}
	if err := b.authorizeTransition(task.State, stage, mutation); err != nil {
		return err
	}
	// A custom workflow may constrain release further, but cannot grant automation shipment authority.
	if mutation.Actor.Automation || (!mutation.Actor.User && mutation.Actor.Member != "release-manager") {
		return fmt.Errorf("shipment requires release-manager or user")
	}
	if err := validateTaskRelease(release); err != nil {
		return err
	}
	before := task.State
	task.State, task.ReleaseVersion, task.ReleaseURL = stage, release.Version, release.URL
	task.ShippedAt = mutation.Now.UTC()
	taskEvent(b, task, mutation, "shipped", before, stage)
	return nil
}

func validateTaskRelease(release *TaskRelease) error {
	if release == nil || !reportText(release.Version, 200) || strings.TrimSpace(release.Version) != release.Version {
		return fmt.Errorf("shipment requires nonempty bounded release version")
	}
	if release.URL != "" {
		u, err := url.Parse(release.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(release.URL) > 2000 {
			return fmt.Errorf("release URL must be an HTTP(S) URL")
		}
	}
	return nil
}

// MoveTaskRelease records shipment metadata while applying the same evidence and actor guards.
func (b *TaskBoard) MoveTaskRelease(id, stage string, release *TaskRelease, mutation TaskMutation) error {
	if b.Category(stage) != "shipped" {
		return b.MoveTask(id, stage, mutation)
	}
	return b.mutate(id, mutation, func(board *TaskBoard, task *Task) error {
		return board.shipTask(task, stage, release, mutation)
	})
}

// CanTransition previews normal move authorization and evidence guards without changing the board.
func (b *TaskBoard) CanTransition(id, stage string, mutation TaskMutation) error {
	candidate := cloneBoard(b)
	return candidate.MoveTask(id, stage, mutation)
}
