package team

import (
	"context"
	"errors"
	"fmt"
)

// AutoReconcile applies what policy allows without a person: assignments always, worker creation
// and retirement only in auto mode. Approvals are never granted here.
func (p *PoolRuntime) AutoReconcile(ctx context.Context) ([]TaskNotice, error) {
	before, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	if before.Revision == 0 {
		return nil, nil
	}
	if p.board.ReadOnly {
		return nil, nil
	}
	if err := p.ReconcileTaskLeases(ctx); err != nil {
		return nil, fmt.Errorf("reconcile task leases before scheduling: %w", err)
	}
	var failures []error
	if err := p.ResumeHealing(ctx); err != nil {
		failures = append(failures, fmt.Errorf("resume automatic healing: %w", err))
	}
	actor := ReconcileActor{Automation: true}
	controller := p.Controller(actor)
	actions, err := controller.Propose(ctx, actor)
	if err != nil {
		return nil, errors.Join(append(failures, fmt.Errorf("plan automatic reconcile: %w", err))...)
	}
	for i := range actions {
		if actions[i].Kind != "assign" && before.Policy.Approval != "auto" && !before.Approved[actions[i].ID] {
			continue
		}
		if _, err := controller.Apply(ctx, actor, actions[i].ID); err != nil {
			failures = append(failures, fmt.Errorf("%s %s: %w", actions[i].Kind, actions[i].Task+actions[i].Worker, err))
		}
	}
	after, err := p.Status(ctx)
	if err != nil {
		return nil, errors.Join(append(failures, err)...)
	}
	manager, err := p.manager(ctx)
	if err != nil {
		return nil, errors.Join(append(failures, err)...)
	}
	notices := TaskNotices(&before.Board, &after.Board, &manager.Team, AutomationActor)
	notices = ownNotices(notices, &after.Board)
	// Briefs are based on durable delivery evidence rather than this pass's state transition.
	// This also recovers assignments started before delivery tracking was introduced.
	kept := withoutBriefs(notices)
	snapshot, err := p.Snapshot(ctx)
	if err != nil {
		return kept, errors.Join(append(failures, err)...)
	}
	kept = append(kept, MissingBriefs(&after.Board, &manager.Team, snapshot.Input.Workers)...)
	return kept, errors.Join(failures...)
}

func withoutBriefs(notices []TaskNotice) []TaskNotice {
	kept := notices[:0]
	for _, notice := range notices {
		if notice.Brief == "" {
			kept = append(kept, notice)
		}
	}
	return kept
}

// ownNotices keeps notices for tasks this pass changed, so concurrent tool changes are not announced twice.
func ownNotices(notices []TaskNotice, board *TaskBoard) []TaskNotice {
	byAutomation := map[string]bool{}
	for i := range board.Tasks {
		history := board.Tasks[i].History
		if len(history) > 0 && history[len(history)-1].Actor == AutomationActor.Member {
			byAutomation[board.Tasks[i].ID] = true
		}
	}
	kept := notices[:0]
	for _, notice := range notices {
		if byAutomation[notice.Task] {
			kept = append(kept, notice)
		}
	}
	return kept
}
