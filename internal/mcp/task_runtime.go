package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/vqivs/agent-deck/internal/team"
)

const (
	maxTaskNotices = 20
	systemSender   = "agent-deck"
)

type autoReconciler interface {
	AutoReconcile(ctx context.Context) ([]team.TaskNotice, error)
}

func (r *Runtime) autoReconcile(ctx context.Context) error {
	if r.options.Scheduler == nil {
		return nil
	}
	auto, ok := r.options.Scheduler.Actions.(autoReconciler)
	if !ok {
		return nil
	}
	if configured, err := r.boardConfigured(ctx); err != nil || !configured {
		return err
	}
	notices, err := auto.AutoReconcile(ctx)
	if len(notices) > 0 {
		err = errors.Join(err, r.Service.Notify(ctx, notices))
	}
	return err
}

func (r *Runtime) boardConfigured(ctx context.Context) (bool, error) {
	_, store, err := r.taskTools().Provider(ctx)
	if err != nil {
		return false, err
	}
	if _, err := store.Load(ctx); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, nil
}

func (r *Runtime) taskTools() *TaskTools {
	if r.options.Tasks != nil {
		return r.options.Tasks
	}
	return &TaskTools{Provider: RuntimePool(r.options.Provider), Notify: r.Service.Notify}
}

// Notify delivers board notices as private agent-deck messages; delivery wakes idle members.
func (s *Service) Notify(ctx context.Context, notices []team.TaskNotice) error {
	if len(notices) > maxTaskNotices {
		return fmt.Errorf("too many task notices in one change (%d)", len(notices))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return fmt.Errorf("channel posting is disabled")
	}
	var failures []error
	for _, notice := range notices {
		if len(notice.Text) > s.options.MaxBytes {
			notice.Text = notice.Text[:s.options.MaxBytes]
		}
		post := Post{Text: notice.Text, Mentions: []string{notice.To}}
		r, err := s.message(ctx, Identity{Team: s.options.Team, Member: systemSender}, post, post.Mentions, true)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		r.Time, r.Hops = s.options.Now().UTC(), 0
		s.bindAudience(&r)
		r.SenderID = systemSender
		if err := s.options.Journal.Append(ctx, &r); err != nil {
			failures = append(failures, fmt.Errorf("notify %s: %w", notice.To, err))
		}
	}
	return errors.Join(failures...)
}
