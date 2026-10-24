package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

// notifyBrief runs under the service notification mutex. A deterministic journal ID closes
// the append/board-save crash gap: after restart the same append is found, then recorded.
func (s *Service) notifyBrief(ctx context.Context, notice *team.TaskNotice) error {
	if s.options.BriefProvider == nil {
		return fmt.Errorf("brief delivery board provider is not configured")
	}
	current, store, err := s.options.BriefProvider(ctx)
	if err != nil {
		return err
	}
	if store.ReadOnly || current.ReadOnly {
		return fmt.Errorf("read-only: brief delivery disabled")
	}
	state, err := store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load brief board: %w", err)
	}
	if err := validateBrief(&state.Board, &current.Team, notice); err != nil {
		return err
	}
	member, err := s.member(Identity{Team: s.options.Team, Member: notice.To})
	if err != nil || member.Muted {
		return fmt.Errorf("brief recipient absent or muted")
	}
	if !briefMemberMatches(&current.Team, &member) {
		return fmt.Errorf("brief recipient identity changed")
	}
	if err := s.appendBrief(ctx, notice, &member); err != nil {
		return err
	}
	err = team.RecordBriefDelivery(context.WithoutCancel(ctx), store, &current.Team, notice, s.options.Now())
	if err != nil {
		return fmt.Errorf("record brief delivery: %w", err)
	}
	return nil
}

func briefMemberMatches(current *team.Team, member *Member) bool {
	for i := range current.Members {
		binding := &current.Members[i]
		if binding.Name == member.Name {
			return binding.ID == member.ID && !binding.Muted && binding.Session != ""
		}
	}
	return false
}

func (s *Service) appendBrief(ctx context.Context, notice *team.TaskNotice, member *Member) error {
	id := "brief:" + notice.Brief
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read brief journal: %w", err)
	}
	found := false
	for i := range records {
		r := &records[i]
		if r.ID == id {
			if r.Sender != systemSender || !r.Private || r.Audience[notice.To] != memberID(member) {
				return fmt.Errorf("brief journal identity mismatch")
			}
			found = true
		}
	}
	if !found {
		post := Post{Text: strings.ToValidUTF8(notice.Text, ""), Mentions: []string{notice.To}}
		if len(post.Text) > s.options.MaxBytes {
			post.Text = strings.ToValidUTF8(post.Text[:s.options.MaxBytes], "")
		}
		r, err := s.message(ctx, Identity{Team: s.options.Team, Member: systemSender}, post, post.Mentions, true)
		if err != nil {
			return err
		}
		r.ID, r.Time, r.Hops, r.SenderID = id, s.options.Now().UTC(), 0, systemSender
		s.bindAudience(&r)
		if err := s.options.Journal.Append(ctx, &r); err != nil {
			return fmt.Errorf("append assignment brief: %w", err)
		}
	}
	return nil
}

func validateBrief(board *team.TaskBoard, current *team.Team, notice *team.TaskNotice) error {
	for i := range board.Tasks {
		task := &board.Tasks[i]
		if task.ID == notice.Task && task.Assignee == notice.To &&
			team.BriefDeliveryKey(task, current) == notice.Brief && notice.Text == team.TaskBrief(task) {
			return nil
		}
	}
	return fmt.Errorf("brief no longer matches current task assignment")
}
