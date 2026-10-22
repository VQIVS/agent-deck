package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/team"
)

func (u *teamUI) formScreen() string {
	if len(u.Fields) > 0 && u.Fields[0].Label == "Member name" {
		return "member-form"
	}
	return "team-form"
}

func (m *Model) teamForm() {
	u := m.teams
	t := &u.Draft
	u.Fields = []formField{
		{Label: "Team name", Value: t.Name},
		{Label: "Server URL", Value: t.Server},
		{Label: "Project repository directory", Value: t.Repo},
		{Label: "Base branch", Value: t.Base},
		{Label: "Default model (provider/model)", Value: t.Model, Choices: u.Models},
		{Label: "Maximum parallel workers", Value: strconv.Itoa(t.Capacity)},
		{Label: "Team rules", Value: t.Rules},
		{Label: "Roster template", Value: t.RosterTemplate},
	}
	u.Field = 0
	u.Screen = "team-form"
	m.input.SetValue(u.Fields[0].Value)
	m.input.Focus()
}

func (m *Model) memberForm() {
	u := m.teams
	member := &u.Draft.Members[u.MemberIndex]
	preset := member.Capability
	if preset == "" && member.Mode == "adopt" && len(member.Permissions) == 0 {
		preset = "inherited"
	}
	u.Fields = []formField{
		{Label: "Member name", Value: member.Name},
		{Label: "Role", Value: member.Role, Choices: []string{"master", "worker"}},
		{Label: "Model", Value: member.Model, Choices: u.Models},
		{Label: "OpenCode agent", Value: member.Agent, Choices: u.Agents},
		{Label: "System prompt", Value: member.System},
		{Label: "System prompt file", Value: member.SystemFile},
		{Label: "Brief", Value: member.Brief},
		{Label: "Workspace mode", Value: member.Mode, Choices: []string{"worktree", "shared", "adopt"}},
		{Label: "Directory", Value: member.Dir},
		{Label: "Branch template", Value: member.BranchTemplate},
		{Label: "Existing session ID (adopt)", Value: member.Session},
		{Label: "Permission preset", Value: preset, Choices: []string{"inherited", "reviewer", "coder", "full"}},
	}
	for _, k := range []string{"read", "edit", "bash", "webfetch", "external_directory"} {
		u.Fields = append(u.Fields, formField{
			Label: "Permission " + k, Value: member.Permissions[k], Choices: []string{"allow", "ask", "deny"},
		})
	}
	u.Field = 0
	u.Screen = "member-form"
	m.input.SetValue(u.Fields[0].Value)
	m.input.Focus()
}

func (m *Model) saveForm() tea.Cmd {
	u := m.teams
	wasMember := u.Screen == "member-form"
	newMember := wasMember && u.Manager != nil && u.MemberIndex >= len(u.Manager.Team.Members)
	masterChanged := u.masterChanged(wasMember)
	u.Fields[u.Field].Value = m.input.Value()
	values := map[string]string{}
	for _, f := range u.Fields {
		values[f.Label] = f.Value
	}
	if u.Screen == "team-form" {
		m.applyTeamValues(values)
	} else if !m.applyMemberValues(values) {
		return nil
	}
	var version []byte
	if u.Manager != nil {
		if u.Draft.Name != u.Manager.Team.Name {
			m.note = "Use Duplicate to change team name"
			return nil
		}
		version = u.Manager.Version
	}
	v, err := u.Store.Save(m.context(), &u.Draft, version)
	if err != nil {
		m.note = err.Error()
		return nil
	}
	u.Manager = &team.Manager{Store: u.Store, Team: u.Draft, Version: v, ReadOnly: m.opts.ReadOnly}
	u.Screen = "members"
	m.note = "Saved · select member and Start to create resources"
	if newMember && u.Draft.Members[u.MemberIndex].Mode != "adopt" {
		u.Confirm = "start"
	}
	if wasMember && masterChanged {
		return m.teamOperation("roster", 0, false)
	}
	return nil
}

func (u *teamUI) masterChanged(wasMember bool) bool {
	return wasMember && u.Manager != nil && u.MemberIndex < len(u.Manager.Team.Members) &&
		u.Manager.Team.Members[u.MemberIndex].Role != u.Draft.Members[u.MemberIndex].Role
}

func (m *Model) applyTeamValues(values map[string]string) {
	t := &m.teams.Draft
	t.Name = values["Team name"]
	t.Server = values["Server URL"]
	t.Repo = values["Project repository directory"]
	t.Base = values["Base branch"]
	t.Model = values["Default model (provider/model)"]
	t.Capacity, _ = strconv.Atoi(values["Maximum parallel workers"])
	t.Rules = values["Team rules"]
	t.RosterTemplate = values["Roster template"]
	for i := range t.Members {
		if t.Members[i].Dir == "" && t.Members[i].Mode == "shared" {
			t.Members[i].Dir = t.Repo
		}
	}
}

func (m *Model) applyMemberValues(values map[string]string) bool {
	u := m.teams
	member := &u.Draft.Members[u.MemberIndex]
	oldMode := member.Mode
	member.Name = values["Member name"]
	role := values["Role"]
	if role == "master" {
		for i := range u.Draft.Members {
			u.Draft.Members[i].Role = "worker"
		}
	}
	member.Role = role
	member.Model = values["Model"]
	member.Agent = values["OpenCode agent"]
	member.System = values["System prompt"]
	member.SystemFile = values["System prompt file"]
	member.Brief = values["Brief"]
	member.Mode = values["Workspace mode"]
	member.Dir = values["Directory"]
	member.BranchTemplate = values["Branch template"]
	member.Session = values["Existing session ID (adopt)"]
	if u.Manager != nil && u.MemberIndex < len(u.Manager.Team.Members) {
		old := &u.Manager.Team.Members[u.MemberIndex]
		if old.Session != "" && (member.Dir != old.Dir || member.Session != old.Session || member.Mode != old.Mode) {
			m.note = "Remove an active member before changing its workspace/session"
			return false
		}
	}
	applyPermissionValues(member, oldMode, values)
	return true
}

func applyPermissionValues(member *team.Member, oldMode string, values map[string]string) {
	member.Permissions = map[string]string{}
	preset := values["Permission preset"]
	if member.Mode == "adopt" && oldMode != "adopt" && preset == "" {
		preset = "inherited"
	}
	for _, k := range []string{"read", "edit", "bash", "webfetch", "external_directory"} {
		member.Permissions[k] = values["Permission "+k]
	}
	if preset != "" {
		member.Permissions = team.Preset(preset)
	}
	member.Capability = preset
	if member.Mode == "adopt" && preset == "" {
		hasRules := false
		for _, v := range member.Permissions {
			if v != "" {
				hasRules = true
			}
		}
		if !hasRules {
			member.Capability = "inherited"
			member.Permissions = nil
		}
	}
}
