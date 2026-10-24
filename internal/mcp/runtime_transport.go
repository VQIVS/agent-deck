package mcp

import (
	"context"
	"fmt"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/team"
)

func (r *Runtime) current(ctx context.Context) (*team.Manager, error) {
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("runtime is closed")
	}
	manager, err := r.options.Provider(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh runtime team: %w", err)
	}
	r.mu.RLock()
	name := r.manager.Team.Name
	r.mu.RUnlock()
	if manager == nil || manager.Team.Name != name {
		return nil, fmt.Errorf("runtime provider changed team identity")
	}
	return manager, nil
}

func (r *Runtime) members() []Member {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Member, 0, len(r.manager.Team.Members))
	for i := range r.manager.Team.Members {
		m := &r.manager.Team.Members[i]
		id := m.Session
		if id == "" {
			id = "unstarted:" + m.Name
		}
		result = append(result, Member{
			ID: id, Name: m.Name, Role: m.Role,
			Muted: r.failed || (r.options.Muted != nil && r.options.Muted(m.Name)),
		})
	}
	return result
}

func (r *Runtime) readOnly() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed || r.failed || r.manager.ReadOnly
}

func (r *Runtime) instructions(ctx context.Context, name string) (string, error) {
	manager, err := r.current(ctx)
	if err != nil {
		return "", err
	}
	index, err := manager.MemberIndex(name)
	if err != nil {
		return "", fmt.Errorf("find delivery recipient: %w", err)
	}
	options, err := team.Options(ctx, &manager.Team, &manager.Team.Members[index], "")
	if err != nil {
		return "", fmt.Errorf("build delivery rules: %w", err)
	}
	roster, err := manager.Team.Roster("channel delivery")
	if err != nil {
		return "", fmt.Errorf("build delivery roster: %w", err)
	}
	return options.System + "\n" + roster +
		"\nUse team_members, channel_post(text, mentions?), channel_read(since?), dm(member, text). " +
		"Only explicit mentions/DMs wake recipients. Deduplicate repeated message IDs.", nil
}

func (r *Runtime) Busy(ctx context.Context, name string) (bool, error) {
	manager, err := r.current(ctx)
	if err != nil {
		return true, err
	}
	return runtimeBusy(ctx, manager, name)
}

func runtimeBusy(ctx context.Context, manager *team.Manager, name string) (bool, error) {
	index, err := manager.MemberIndex(name)
	if err != nil {
		return true, fmt.Errorf("find runtime member: %w", err)
	}
	m := &manager.Team.Members[index]
	if m.Session == "" || m.Dir == "" {
		return true, nil
	}
	client := api.New(manager.Team.Server, true)
	statuses, err := client.Status(ctx, m.Dir)
	if err != nil {
		return true, fmt.Errorf("read scoped runtime status: %w", err)
	}
	if status := statuses[m.Session].Type; status != "" && status != "idle" {
		return true, nil
	}
	sessions, err := client.Sessions(ctx, m.Dir)
	if err != nil {
		return true, fmt.Errorf("read scoped runtime sessions: %w", err)
	}
	found := false
	for _, session := range sessions {
		if session.ID == m.Session {
			found = true
		}
	}
	if !found {
		return true, fmt.Errorf("runtime member session is missing")
	}
	return runtimeWaiting(ctx, client, m)
}

func runtimeWaiting(ctx context.Context, client *api.Client, m *team.Member) (bool, error) {
	permissions, err := client.Permissions(ctx, m.Dir)
	if err != nil {
		return true, fmt.Errorf("read scoped runtime permissions: %w", err)
	}
	for _, request := range permissions {
		if request.SessionID == m.Session {
			return true, nil
		}
	}
	questions, err := client.Questions(ctx, m.Dir)
	if err != nil {
		return true, fmt.Errorf("read scoped runtime questions: %w", err)
	}
	for _, request := range questions {
		if request.SessionID == m.Session {
			return true, nil
		}
	}
	return false, nil
}

func (r *Runtime) Send(ctx context.Context, name, prompt string) error {
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	if manager.ReadOnly || (r.options.Muted != nil && r.options.Muted(name)) {
		return fmt.Errorf("runtime delivery is disabled")
	}
	busy, err := runtimeBusy(ctx, manager, name)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("runtime recipient is no longer idle; delivery remains queued")
	}
	index, err := manager.MemberIndex(name)
	if err != nil {
		return fmt.Errorf("find runtime recipient: %w", err)
	}
	if err := manager.Send(ctx, index, prompt); err != nil {
		return fmt.Errorf("send runtime prompt: %w", err)
	}
	return nil
}
