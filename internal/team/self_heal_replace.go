package team

import (
	"context"
	"fmt"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func (t *LifecycleTransport) replaceWorker(ctx context.Context, manager *Manager, index int,
	effect *lifecycleEffect,
) (ReconcileResult, error) {
	member := &manager.Team.Members[index]
	result := ReconcileResult{Worker: member.Name, Session: member.Session}
	if t.WorkerSetup == nil {
		return result, fmt.Errorf("replacement requires durable worker identity setup")
	}
	previous := member.Session
	member.Session = ""
	err := manager.Team.CanStart(index)
	member.Session = previous
	if err != nil {
		return result, err
	}
	if effect.State == "healed" {
		return result, nil
	}
	if err := t.prepareReplacement(ctx, manager, index, effect); err != nil {
		return result, err
	}
	setupErr := t.WorkerSetup(ctx, manager, index)
	member = &manager.Team.Members[index]
	if member.ID != effect.Request.Member.ID {
		return result, fmt.Errorf("replacement setup changed member identity")
	}
	bound := *member
	effect.Binding = &bound
	if err := t.saveHealing(ctx, effect, "unbound"); err != nil {
		return result, err
	}
	if setupErr != nil {
		return result, fmt.Errorf("set up replacement worker: %w", setupErr)
	}
	if member.Session == "" || member.Session == effect.Request.Member.Session {
		return result, fmt.Errorf("replacement setup must bind a fresh session")
	}
	result.Session = member.Session
	if err := t.saveHealing(ctx, effect, "sending"); err != nil {
		return result, err
	}
	if err := manager.Send(ctx, index, effect.Handoff); err != nil {
		return result, fmt.Errorf("send durable replacement handoff: %w", err)
	}
	return result, t.saveHealing(ctx, effect, "healed")
}

func (t *LifecycleTransport) prepareReplacement(ctx context.Context, manager *Manager, index int,
	effect *lifecycleEffect,
) error {
	member := &manager.Team.Members[index]
	if effect.State == "started" {
		if err := t.prepareHandoff(ctx, manager, index, effect); err != nil {
			return err
		}
	}
	if effect.State == "prepared" {
		if effect.Request.Action.Kind != "recover" {
			if err := t.saveHealing(ctx, effect, "aborting"); err != nil {
				return err
			}
			if err := api.New(manager.Team.Server, false).Abort(ctx, member.SessionDir(), member.Session); err != nil {
				return fmt.Errorf("abort replaced worker: %w", err)
			}
		}
		if err := t.saveHealing(ctx, effect, "aborted"); err != nil {
			return err
		}
	}
	if effect.State == "aborted" {
		if err := t.clearReplacementBinding(ctx, manager, index, effect); err != nil {
			return err
		}
		if err := t.saveHealing(ctx, effect, "unbound"); err != nil {
			return err
		}
	}
	return nil
}

func (t *LifecycleTransport) clearReplacementBinding(ctx context.Context, manager *Manager,
	index int, effect *lifecycleEffect,
) error {
	member := &manager.Team.Members[index]
	if member.Session != effect.Request.Member.Session {
		return nil
	}
	if effect.Request.Action.Kind != "recover" && manager.Observe(ctx, index).State != "idle" {
		return fmt.Errorf("replaced worker has not stopped")
	}
	member.PreviousSession, member.PreviousDir = member.Session, member.SessionDir()
	member.Session, member.ControlDir = "", ""
	member.Leases, member.LeasePolicy = nil, ""
	bound := *member
	effect.Binding = &bound
	if err := t.saveHealing(ctx, effect, "aborted"); err != nil {
		return err
	}
	return manager.Save(ctx)
}

func (t *LifecycleTransport) prepareHandoff(ctx context.Context, manager *Manager, index int,
	effect *lifecycleEffect,
) error {
	member := &manager.Team.Members[index]
	if member.Session != effect.Request.Member.Session {
		return fmt.Errorf("replacement binding changed")
	}
	status := manager.Observe(ctx, index)
	if status.State == "waiting" {
		return fmt.Errorf("replacement cannot interrupt pending requests")
	}
	if effect.Request.Action.Kind == "recover" {
		if status.State != "offline" || !strings.Contains(status.Error, "is missing") {
			return fmt.Errorf("offline recovery requires a confirmed missing session")
		}
		effect.Handoff = "Prior pool session is missing; recover open tasks without claiming completion."
	} else {
		text, err := WorkflowHandoff(ctx, manager, index)
		if err != nil {
			return err
		}
		effect.Handoff = text
	}
	state, err := t.Store.LoadUnlocked(ctx)
	if err != nil {
		return err
	}
	var tasks strings.Builder
	tasks.WriteString("\nDurable active task handoff (not completed):\n")
	for i := range state.Board.Tasks {
		task := &state.Board.Tasks[i]
		if TaskActive(task) && task.Assignee == member.Name {
			fmt.Fprintf(&tasks, "- %s [%s] %s\n", task.ID, task.State, task.Title)
		}
	}
	effect.Handoff += tasks.String()
	return t.saveHealing(ctx, effect, "prepared")
}
