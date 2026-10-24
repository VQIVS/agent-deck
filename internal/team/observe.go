package team

import (
	"context"
	"fmt"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// MemberStatus is a directory-scoped snapshot of one configured member.
type MemberStatus struct {
	Name          string `json:"name"`
	Role          string `json:"role"`
	Session       string `json:"session"`
	Directory     string `json:"directory"`
	State         string `json:"state"`
	Pending       int    `json:"pending"`
	ContextTokens int64  `json:"context_tokens"`
	LastActivity  int64  `json:"last_activity"`
	Error         string `json:"error,omitempty"`
}

// PendingRequest binds a request to its configured member and directory.
type PendingRequest struct {
	Member     string          `json:"member"`
	Directory  string          `json:"directory"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Permission *api.Permission `json:"permission,omitempty"`
	Question   *api.Question   `json:"question,omitempty"`
}

func (m *Manager) MemberIndex(name string) (int, error) {
	for i := range m.Team.Members {
		if m.Team.Members[i].Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown member %q", name)
}

func (m *Manager) Requests(ctx context.Context) ([]PendingRequest, error) {
	client := api.New(m.Team.Server, m.ReadOnly)
	out := []PendingRequest{}
	dirs := map[string]bool{}
	for i := range m.Team.Members {
		member := &m.Team.Members[i]
		dir := member.SessionDir()
		if member.Session == "" || dirs[dir] {
			continue
		}
		dirs[dir] = true
		permissions, err := client.Permissions(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("permissions in %q: %w", member.Dir, err)
		}
		questions, err := client.Questions(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("questions in %q: %w", member.Dir, err)
		}
		out = append(out, m.matchRequests(dir, permissions, questions)...)
	}
	return out, nil
}

func (m *Manager) matchRequests(dir string, permissions []api.Permission, questions []api.Question) []PendingRequest {
	var out []PendingRequest
	for i := range m.Team.Members {
		member := &m.Team.Members[i]
		if member.SessionDir() != dir || member.Session == "" {
			continue
		}
		for j := range permissions {
			r := &permissions[j]
			if r.SessionID == member.Session {
				out = append(out, PendingRequest{member.Name, dir, r.ID, "permission", r, nil})
			}
		}
		for j := range questions {
			r := &questions[j]
			if r.SessionID == member.Session {
				out = append(out, PendingRequest{member.Name, dir, r.ID, "question", nil, r})
			}
		}
	}
	return out
}

func (m *Manager) Observe(ctx context.Context, index int) MemberStatus {
	member := &m.Team.Members[index]
	out := MemberStatus{
		Name: member.Name, Role: member.Role, Session: member.Session,
		Directory: member.SessionDir(), State: "offline",
	}
	if member.Session == "" {
		return out
	}
	if err := m.observe(ctx, member, &out); err != nil {
		out.State, out.Error = "offline", err.Error()
	}
	return out
}

func (m *Manager) observe(ctx context.Context, member *Member, out *MemberStatus) error {
	client := api.New(m.Team.Server, true)
	statuses, err := client.Status(ctx, member.SessionDir())
	if err != nil {
		return fmt.Errorf("read member status: %w", err)
	}
	sessions, err := client.Sessions(ctx, member.SessionDir())
	if err != nil {
		return fmt.Errorf("read member session: %w", err)
	}
	found := false
	for _, session := range sessions {
		if session.ID == member.Session {
			found, out.LastActivity = true, session.Time.Updated
		}
	}
	if !found {
		return fmt.Errorf("session %q is missing", member.Session)
	}
	permissions, err := client.Permissions(ctx, member.SessionDir())
	if err != nil {
		return fmt.Errorf("read member permissions: %w", err)
	}
	questions, err := client.Questions(ctx, member.SessionDir())
	if err != nil {
		return fmt.Errorf("read member questions: %w", err)
	}
	out.Pending = countRequests(permissions, questions, member.Session)
	out.State = "idle"
	if status := statuses[member.Session].Type; status != "" && status != "idle" {
		out.State = "busy"
	}
	if out.Pending > 0 {
		out.State = "waiting"
	}
	messages, err := client.Messages(ctx, member.SessionDir(), member.Session)
	if err != nil {
		return fmt.Errorf("read member messages: %w", err)
	}
	if latest := ContextAssistant(messages); latest != nil {
		out.ContextTokens = latest.Info.Tokens.Input + latest.Info.Tokens.Cache.Read
	}
	return nil
}

func countRequests(permissions []api.Permission, questions []api.Question, session string) int {
	count := 0
	for _, request := range permissions {
		if request.SessionID == session {
			count++
		}
	}
	for _, request := range questions {
		if request.SessionID == session {
			count++
		}
	}
	return count
}

func LatestAssistant(messages []api.Message) *api.Message {
	var latest *api.Message
	for i := range messages {
		message := &messages[i]
		if message.Info.Role == "assistant" && (latest == nil || message.Info.Time.Created >= latest.Info.Time.Created) {
			latest = message
		}
	}
	return latest
}

func AssistantText(messages []api.Message) string {
	latest := LatestAssistant(messages)
	if latest == nil {
		return ""
	}
	var parts []string
	for i := range latest.Parts {
		part := &latest.Parts[i]
		if part.Type == "text" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func ContextAssistant(messages []api.Message) *api.Message {
	latest := LatestAssistant(messages)
	if latest == nil || latest.Info.Time.Completed > 0 || latest.Info.Tokens.Input+latest.Info.Tokens.Cache.Read > 0 {
		return latest
	}
	var completed *api.Message
	for i := range messages {
		message := &messages[i]
		if message.Info.Role == "assistant" && message.Info.Time.Completed > 0 &&
			(completed == nil || message.Info.Time.Created >= completed.Info.Time.Created) {
			completed = message
		}
	}
	return completed
}
