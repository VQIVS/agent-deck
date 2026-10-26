package team

import (
	"context"
	"fmt"
	"os"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// Manager coordinates a team's local persistence and explicit live-session operations.
type Manager struct {
	Store    Store
	Team     Team
	Version  []byte
	ReadOnly bool
}

func (m *Manager) Save(ctx context.Context) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only: team changes disabled")
	}
	v, e := m.Store.Save(ctx, &m.Team, m.Version)
	if e == nil {
		m.Version = v
	}
	return e
}

func Options(ctx context.Context, t *Team, member *Member, text string) (api.PromptOptions, error) {
	if err := ctx.Err(); err != nil {
		return api.PromptOptions{}, err
	}
	system := member.System
	if member.SystemFile != "" {
		b, e := os.ReadFile(member.SystemFile)
		if e != nil {
			return api.PromptOptions{}, fmt.Errorf("read system prompt %q: %w", member.SystemFile, e)
		}
		system = string(b)
	}
	if member.ControlDir != "" {
		system += fmt.Sprintf("\nSession control directory: %q. Code checkout: %q. "+
			"Use absolute code paths and checkout workdir. "+
			"Never write source in the control directory. Previous transcript: session=%q directory=%q. "+
			"Member credentials provide cooperative same-user identity, not a sandbox.",
			member.ControlDir, member.Dir, member.PreviousSession, member.PreviousDir)
	}
	system += configuredWorkflowInstructions(t)
	o := api.PromptOptions{
		System: strings.TrimSpace(t.Rules + "\n\n" + system + leaseInstructions(t) + workflowInstructions(t, member)),
		Agent:  member.Agent,
		Parts:  []map[string]string{{"type": "text", "text": text}},
	}
	model := member.Model
	if model == "" {
		model = t.Model
	}
	if model != "" {
		provider, id, ok := strings.Cut(model, "/")
		if !ok {
			return o, fmt.Errorf("model must be provider/model")
		}
		o.Model = &api.ModelRef{ProviderID: provider, ModelID: id}
	}
	return o, nil
}

func configuredWorkflowInstructions(t *Team) string {
	if !t.LeasePool && len(t.Workflow.Stages) == 0 {
		return ""
	}
	definition := t.Workflow.Resolved()
	var text strings.Builder
	text.WriteString("\n\nConfigured task workflow (stage names are exact; categories define behavior):\n")
	for _, stage := range definition.Stages {
		fmt.Fprintf(&text, "- %q: %s\n", stage.Name, stage.Category)
	}
	text.WriteString("Discover currently authorized moves using task_workflow (or task workflow CLI). " +
		"Use configured stage names; changing a stage name never grants acceptance or release authority. " +
		"Shipment requires previously accepted work, an explicit version, and authorized release-manager/user. " +
		"pending_release lists accepted work waiting for shipment. Completion and shipment are distinct milestones.\n")
	return text.String()
}

func (m *Manager) Send(ctx context.Context, index int, text string) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only")
	}
	member := m.Team.Members[index]
	if member.Session == "" {
		return fmt.Errorf("member %q has no session", member.Name)
	}
	o, e := Options(ctx, &m.Team, &member, text)
	if e != nil {
		return e
	}
	if err := api.New(m.Team.Server, false).PromptWith(ctx, member.SessionDir(), member.Session, o); err != nil {
		return fmt.Errorf("send to member %q: %w", member.Name, err)
	}
	return nil
}

func (m *Manager) Roster(ctx context.Context, reason string) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only")
	}
	text, e := m.Team.Roster(reason)
	if e != nil {
		return e
	}
	for i := range m.Team.Members {
		member := &m.Team.Members[i]
		if member.Role == "master" && member.Session != "" {
			return m.Send(ctx, i, text)
		}
	}
	return nil
}

func (m *Manager) Start(ctx context.Context, index int) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only")
	}
	if e := m.Team.CanStart(index); e != nil {
		return e
	}
	if e := m.Team.Validate(); e != nil {
		return e
	}
	member := &m.Team.Members[index]
	if member.Capability == "inherited" {
		return fmt.Errorf("choose explicit creation permissions before starting a new session")
	}
	if _, e := Options(ctx, &m.Team, member, member.Brief); e != nil {
		return e
	}
	rules, err := m.creationRules(ctx, member)
	if err != nil {
		return err
	}
	s, e := api.New(m.Team.Server, false).CreateMember(ctx, member.SessionDir(), member.Name, rules)
	if e != nil {
		return fmt.Errorf("create member %q: %w", member.Name, e)
	}
	member.Session = s.ID
	member.LeasePolicy = m.Team.LeaseFingerprint(member)
	if e := m.Save(ctx); e != nil {
		return e
	}
	if e := m.Send(ctx, index, member.Brief); e != nil {
		return e
	}
	return m.Roster(ctx, "member started: "+member.Name)
}

func (m *Manager) Remove(ctx context.Context, index int) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only")
	}
	if index < 0 || index >= len(m.Team.Members) {
		return fmt.Errorf("unknown member")
	}
	member := m.Team.Members[index]
	if member.Role == "master" {
		return fmt.Errorf("change master before removing this member")
	}
	if member.Session != "" {
		if e := api.New(m.Team.Server, false).Abort(ctx, member.SessionDir(), member.Session); e != nil {
			return fmt.Errorf("abort member %q: %w", member.Name, e)
		}
		if member.ControlDir != "" {
			if err := api.New(m.Team.Server, false).Delete(ctx, member.SessionDir(), member.Session); err != nil {
				return fmt.Errorf("delete removed control session: %w", err)
			}
		}
	}
	m.Team.Members = append(m.Team.Members[:index], m.Team.Members[index+1:]...)
	if e := m.Save(ctx); e != nil {
		return e
	}
	return m.Roster(ctx, "removed member: "+member.Name)
}

func (m *Manager) ChangeMaster(ctx context.Context, index int) error {
	if m.ReadOnly {
		return fmt.Errorf("read-only")
	}
	if index < 0 || index >= len(m.Team.Members) {
		return fmt.Errorf("unknown member")
	}
	for i := range m.Team.Members {
		m.Team.Members[i].Role = "worker"
	}
	m.Team.Members[index].Role = "master"
	if e := m.Save(ctx); e != nil {
		return e
	}
	return m.Roster(ctx, "master changed")
}
