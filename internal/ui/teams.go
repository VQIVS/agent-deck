package ui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/team"
)

type teamUI struct {
	Store                     team.Store
	Manager                   *team.Manager
	Teams                     []team.Team
	Summary                   map[string]string
	Status                    map[string]teamStatus
	Transfer                  transferUI
	RootContext               func() context.Context
	Screen                    string
	Index, MemberIndex, Field int
	Fields                    []formField
	Draft                     team.Team
	Models, Agents            []string
	Confirm                   string
	Force, Busy               bool
	Cancel                    context.CancelFunc
}
type formField struct {
	Label, Value string
	Choices      []string
}
type teamsMsg struct {
	Teams   []team.Team
	Summary map[string]string
	Err     error
	Status  map[string]teamStatus
}
type teamStatus struct {
	Busy, Idle, Waiting, Offline int
	Last                         time.Time
}
type catalogMsg struct {
	Models, Agents []string
	Err            error
}
type teamOperationMsg struct {
	Manager *team.Manager
	Err     error
}
type teamReloadMsg struct {
	Team    team.Team
	Version []byte
	Err     error
}

func (m *Model) EnableTeams(root string) {
	m.teams = &teamUI{
		Store:       team.Store{Root: root, ReadOnly: m.opts.ReadOnly},
		Screen:      "picker",
		Summary:     map[string]string{},
		Status:      map[string]teamStatus{},
		RootContext: m.context,
	}
}

func (m *Model) OpenTeam(t *team.Team, version []byte, root string) {
	m.EnableTeams(root)
	m.teams.Manager = &team.Manager{Store: m.teams.Store, Team: *t, Version: version, ReadOnly: m.opts.ReadOnly}
	m.teams.Screen = ""
}

func (m *Model) listTeams() tea.Cmd {
	store := m.teams.Store
	return func() tea.Msg { teams, err := store.List(m.context()); return teamsMsg{Teams: teams, Err: err} }
}

func (m *Model) teamStatuses(teams []team.Team) tea.Cmd {
	ctx := m.teams.RootContext()
	return func() tea.Msg {
		summaries := map[string]string{}
		stats := map[string]teamStatus{}
		for i := range teams {
			t := &teams[i]
			summary := teamStatus{}
			for j := range t.Members {
				member := &t.Members[j]
				if member.Session == "" {
					continue
				}
				pollMember(ctx, t.Server, member, &summary)
			}
			summaries[t.Name] = fmt.Sprintf(
				"%d members · %d busy · %d idle · %d waiting",
				len(t.Members),
				summary.Busy,
				summary.Idle,
				summary.Waiting,
			)
			stats[t.Name] = summary
		}
		return teamsMsg{Teams: teams, Summary: summaries, Status: stats}
	}
}

func pollMember(ctx context.Context, server string, member *team.Member, summary *teamStatus) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := api.New(server, true)
	statuses, err := client.Status(ctx, member.Dir)
	permissions, _ := client.Permissions(ctx, member.Dir)
	questions, _ := client.Questions(ctx, member.Dir)
	sessions, _ := client.Sessions(ctx, member.Dir)
	for _, session := range sessions {
		if session.ID == member.Session && session.Time.Updated > 0 {
			updated := time.UnixMilli(session.Time.Updated)
			if updated.After(summary.Last) {
				summary.Last = updated
			}
		}
	}
	blocked := false
	for _, request := range permissions {
		blocked = blocked || request.SessionID == member.Session
	}
	for _, request := range questions {
		blocked = blocked || request.SessionID == member.Session
	}
	switch {
	case blocked:
		summary.Waiting++
	case err != nil:
		summary.Offline++
	case statuses[member.Session].Type == "busy", statuses[member.Session].Type == "retry":
		summary.Busy++
	default:
		summary.Idle++
	}
}

func (m *Model) catalog() tea.Cmd {
	u := m.teams
	server, dir := u.Draft.Server, u.Draft.Repo
	return func() tea.Msg {
		models, agents, e := api.New(server, true).Catalog(m.context(), dir)
		return catalogMsg{models, agents, e}
	}
}

func (m *Model) loadDashboard() tea.Cmd {
	u := m.teams
	if u.Manager == nil {
		return nil
	}
	if u.Cancel != nil {
		u.Cancel()
	}
	ctx, cancel := context.WithCancel(u.RootContext())
	u.Cancel = cancel
	deck := u.Manager.Team.Deck()
	n := New(ctx, &deck, m.opts)
	n.skipTick = true
	n.teams = u
	n.opts.Auto = false
	n.w, n.h = m.w, m.h
	*m = *n
	return m.Init()
}

func (m *Model) watchTeam() tea.Cmd {
	if m.teams == nil || m.teams.Manager == nil || m.teams.Busy {
		return nil
	}
	manager := m.teams.Manager
	store := manager.Store
	name := manager.Team.Name
	version := append([]byte(nil), manager.Version...)
	return func() tea.Msg {
		t, b, e := store.Load(m.context(), name)
		if e == nil && bytes.Equal(version, b) {
			return nil
		}
		return teamReloadMsg{t, b, e}
	}
}

func (m *Model) teamOperation(operation string, index int, force bool) tea.Cmd {
	u := m.teams
	if m.opts.ReadOnly || u.Busy {
		return nil
	}
	u.Busy = true
	// Own a detached manager copy while the command performs network/filesystem IO.
	manager := *u.Manager
	manager.Team.Members = append([]team.Member(nil), manager.Team.Members...)
	return func() tea.Msg {
		var e error
		switch operation {
		case "start":
			e = manager.Start(m.context(), index)
		case "remove":
			e = manager.Remove(m.context(), index, force)
		case "master":
			e = manager.ChangeMaster(m.context(), index)
		case "roster":
			e = manager.Roster(m.context(), "manual roster refresh")
		}
		return teamOperationMsg{&manager, e}
	}
}

func (m *Model) teamContent() string {
	u := m.teams
	m.zones = nil
	if u.Confirm == "" && (strings.HasPrefix(u.Screen, "import") || u.Screen == "export") {
		return m.transferView()
	}
	if u.Confirm != "" {
		return m.teamConfirmation()
	}
	switch u.Screen {
	case "members":
		return m.membersView()
	case "team-form", "member-form":
		return m.formView()
	case "choices":
		return m.choicesView()
	default:
		return ""
	}
}

func (m *Model) teamConfirmation() string {
	u := m.teams
	text := u.Confirm
	if text == "remove" {
		text = "Remove member (abort session and eligible worktree)?"
	}
	if text == "delete" {
		text = "Delete team file? Sessions and worktrees are retained."
	}
	if text == "export-replace" {
		text = "Replace export file " + u.Transfer.Path + "?"
	}
	if text == "start" {
		text = "Start member and create its configured resources?"
	}
	if text == "master" {
		text = "Change master and send the new master an updated roster?"
	}
	out := fit(
		text,
		m.w,
	) + "\n" + m.buttonRow(
		[]button{{"Yes", "team-yes", m.opts.ReadOnly}, {"No", "team-no", false}},
		0,
		1,
		m.w,
		-1,
	)
	if u.Confirm == "remove" {
		out += "\n" + m.buttonRow(
			[]button{{fmt.Sprintf("delete worktree anyway: %t", u.Force), "team-force", m.opts.ReadOnly}},
			0,
			2,
			m.w,
			-1,
		)
	}
	return out
}

func (m *Model) membersView() string {
	u := m.teams
	var b strings.Builder
	b.WriteString(m.theme.title("Team: "+u.Manager.Team.Name) + "\n")
	b.WriteString(
		m.buttonRow(
			[]button{
				{"Settings", "team-settings", m.opts.ReadOnly},
				{"+ Worker", "team-add", m.opts.ReadOnly},
				{"Back", "team-back", false},
				{"Teams", "team-picker", false},
			},
			0,
			1,
			m.w,
			-1,
		) + "\n",
	)
	for i := range u.Manager.Team.Members {
		member := &u.Manager.Team.Members[i]
		if i+3 >= m.h {
			break
		}
		prefix := "  "
		if i == u.MemberIndex {
			prefix = "> "
		}
		text := fit(
			prefix+member.Role+" "+member.Name+" · "+member.PermissionSummary()+" · "+member.Mode+" · "+member.Session,
			m.w,
		)
		m.zones = append(
			m.zones,
			zone{Rect: Rect{0, 2 + i, m.w, 1}, Action: fmt.Sprintf("member-row:%d", i), Pane: -1},
		)
		b.WriteString(text + "\n")
	}
	y := 2 + min(len(u.Manager.Team.Members), m.h-3)
	b.WriteString(
		m.buttonRow(
			[]button{
				{"Edit", "member-edit", m.opts.ReadOnly},
				{"Start", "member-start", m.opts.ReadOnly},
				{"Remove", "member-remove", m.opts.ReadOnly},
				{"Change master", "member-master", m.opts.ReadOnly},
			},
			0,
			y,
			m.w,
			-1,
		) + "\n" + fit(
			m.note,
			m.w,
		),
	)
	return b.String()
}

func (m *Model) formView() string {
	u := m.teams
	var b strings.Builder
	b.WriteString(m.theme.title("Edit "+strings.TrimSuffix(u.Screen, "-form")) + "\n")
	b.WriteString(
		m.buttonRow(
			[]button{
				{"Save", "form-save", m.opts.ReadOnly},
				{"Cancel", "form-cancel", false},
				{"Load models/agents", "form-catalog", false},
			},
			0,
			1,
			m.w,
			-1,
		) + "\n",
	)
	field := u.Fields[u.Field]
	b.WriteString(m.theme.title(fit(field.Label, m.w)) + "\n")
	m.input.SetWidth(max(10, m.w-2))
	m.input.SetHeight(min(6, max(1, m.h-8)))
	height := min(6, max(1, m.h-8))
	m.zones = append(m.zones, zone{Rect: Rect{0, 3, m.w, height}, Action: "input", Pane: -1})
	b.WriteString(m.input.View() + "\n")
	y := 3 + height
	b.WriteString(
		m.buttonRow(
			[]button{
				{"Previous", "form-prev", false},
				{"Next", "form-next", false},
				{"Choose / preset", "form-choice", false},
			},
			0,
			y,
			m.w,
			-1,
		) + "\n",
	)
	hint := m.note
	if u.Screen == "member-form" {
		hint = "Permissions apply only to newly created sessions; live rules are unchanged. · " + hint
	}
	b.WriteString(m.theme.dim(fit(fmt.Sprintf("Field %d/%d · %s", u.Field+1, len(u.Fields), hint), m.w)))
	return b.String()
}

func (m *Model) choicesView() string {
	u := m.teams
	var b strings.Builder
	b.WriteString("Choose " + u.Fields[u.Field].Label + " (click; Esc back)\n")
	choices := u.Fields[u.Field].Choices
	start := max(0, u.Index-max(1, m.h-3)+1)
	for i := start; i < min(len(choices), start+max(1, m.h-3)); i++ {
		prefix := "  "
		if i == u.Index {
			prefix = "> "
		}
		b.WriteString(fit(prefix+choices[i], m.w) + "\n")
		m.zones = append(
			m.zones,
			zone{Rect: Rect{0, 1 + i - start, m.w, 1}, Action: fmt.Sprintf("choice:%d", i), Pane: -1},
		)
	}
	return b.String()
}

func (m *Model) teamAction(action string) tea.Cmd {
	u := m.teams
	if u == nil {
		return nil
	}
	if handled, cmd := m.transferAction(action); handled {
		return cmd
	}
	if u.Busy {
		m.note = "Team operation in progress"
		return nil
	}
	if m.teamSelection(action) {
		return nil
	}
	if handled, cmd := m.teamNavigation(action); handled {
		return cmd
	}
	if handled, cmd := m.formAction(action); handled {
		return cmd
	}
	if m.opts.ReadOnly {
		m.note = "Read-only: team changes disabled"
		return nil
	}
	return m.mutateTeam(action)
}

func (m *Model) teamSelection(action string) bool {
	u := m.teams
	if strings.HasPrefix(action, "team-row:") {
		_, _ = fmt.Sscanf(action, "team-row:%d", &u.Index)
		return true
	}
	if strings.HasPrefix(action, "member-row:") {
		_, _ = fmt.Sscanf(action, "member-row:%d", &u.MemberIndex)
		return true
	}
	if strings.HasPrefix(action, "choice:") {
		i := 0
		_, _ = fmt.Sscanf(action, "choice:%d", &i)
		choices := u.Fields[u.Field].Choices
		if i >= 0 && i < len(choices) {
			m.input.SetValue(choices[i])
			u.Fields[u.Field].Value = choices[i]
		}
		if u.Screen == "choices" {
			u.Screen = u.formScreen()
		}
		return true
	}
	return false
}

func (m *Model) teamNavigation(action string) (bool, tea.Cmd) {
	u := m.teams
	if action == "team-all" {
		u.Screen = ""
		u.Manager = nil
		m.opts.Auto = true
		return true, m.discover()
	}
	if action == "team-list" {
		return true, m.listTeams()
	}
	if action == "team-picker" {
		u.Screen = "picker"
		return true, m.listTeams()
	}
	if action == "team-back" {
		u.Screen = ""
		return true, m.loadDashboard()
	}
	if action == "team-members" {
		u.Screen = "members"
		return true, nil
	}
	if action == "team-open" {
		if len(u.Teams) == 0 {
			return true, nil
		}
		t, v, e := u.Store.Load(m.context(), u.Teams[u.Index].Name)
		if e != nil {
			m.note = e.Error()
			return true, nil
		}
		u.Manager = &team.Manager{Store: u.Store, Team: t, Version: v, ReadOnly: m.opts.ReadOnly}
		u.Screen = ""
		return true, m.loadDashboard()
	}
	if action == "team-no" {
		u.Confirm = ""
		return true, nil
	}
	return false, nil
}

func (m *Model) formAction(action string) (bool, tea.Cmd) {
	u := m.teams
	if action == "form-cancel" {
		u.Screen = "members"
		if u.Manager == nil {
			u.Screen = "picker"
		}
		return true, nil
	}
	if action == "form-next" || action == "form-prev" {
		u.Fields[u.Field].Value = m.input.Value()
		delta := 1
		if action == "form-prev" {
			delta = -1
		}
		u.Field = (u.Field + delta + len(u.Fields)) % len(u.Fields)
		m.input.SetValue(u.Fields[u.Field].Value)
		return true, nil
	}
	if action == "form-choice" {
		u.Fields[u.Field].Value = m.input.Value()
		if len(u.Fields[u.Field].Choices) > 0 {
			u.Screen = "choices"
			u.Index = 0
		}
		return true, nil
	}
	if action == "form-save" && u.Screen == "choices" {
		u.Screen = u.formScreen()
	}
	if action == "form-catalog" {
		return true, m.catalog()
	}
	return false, nil
}

func (m *Model) mutateTeam(action string) tea.Cmd {
	u := m.teams
	switch action {
	case "team-new":
		u.Manager = nil
		u.Draft = team.Team{
			Version:  1,
			Name:     "New team",
			Server:   "http://127.0.0.1:4096",
			Base:     "main",
			Capacity: 4,
			Members: []team.Member{
				{Name: "Master", Role: "master", Mode: "shared", Agent: "build", Permissions: team.Preset("coder")},
			},
		}
		m.teamForm()
		return nil
	case "team-edit":
		m.editTeam()
	case "team-settings":
		u.Draft = u.Manager.Team
		m.teamForm()
	case "team-duplicate":
		m.duplicateTeam()
	case "team-delete":
		u.Confirm = "delete"
	case "team-force":
		u.Force = !u.Force
	case "team-yes":
		return m.confirmTeam()
	case "team-add":
		u.Draft = u.Manager.Team
		u.Draft.Members = append(
			append([]team.Member(nil), u.Draft.Members...),
			team.Member{
				Name:        fmt.Sprintf("Worker %d", len(u.Draft.Members)),
				Role:        "worker",
				Mode:        "worktree",
				Agent:       "build",
				Permissions: team.Preset("coder"),
			},
		)
		u.MemberIndex = len(u.Draft.Members) - 1
		m.memberForm()
	case "member-edit":
		u.Draft = u.Manager.Team
		u.Draft.Members = append([]team.Member(nil), u.Draft.Members...)
		m.memberForm()
	case "member-start":
		u.Confirm = "start"
	case "member-remove":
		u.Confirm = "remove"
		u.Force = false
	case "member-master":
		u.Confirm = "master"
	case "form-save":
		return m.saveForm()
	}
	return nil
}

func (m *Model) editTeam() {
	u := m.teams
	if len(u.Teams) == 0 {
		return
	}
	t, revision, err := u.Store.Load(m.context(), u.Teams[u.Index].Name)
	if err != nil {
		m.note = err.Error()
		return
	}
	u.Manager = &team.Manager{Store: u.Store, Team: t, Version: revision}
	u.Draft = t
	m.teamForm()
}

func (m *Model) duplicateTeam() {
	u := m.teams
	if len(u.Teams) == 0 {
		return
	}
	u.Draft = u.Teams[u.Index]
	u.Draft.Members = append([]team.Member(nil), u.Draft.Members...)
	u.Draft.Name += " copy"
	for i := range u.Draft.Members {
		u.Draft.Members[i].Session = ""
		u.Draft.Members[i].Owned = false
		u.Draft.Members[i].Branch = ""
	}
	u.Manager = nil
	m.teamForm()
}

func (m *Model) confirmTeam() tea.Cmd {
	u := m.teams
	confirm := u.Confirm
	u.Confirm = ""
	if confirm == "delete" && len(u.Teams) > 0 {
		if err := u.Store.Delete(m.context(), u.Teams[u.Index].Name); err != nil {
			m.note = err.Error()
		}
		u.Index = 0
		return m.listTeams()
	}
	switch confirm {
	case "remove":
		return m.teamOperation("remove", u.MemberIndex, u.Force)
	case "start", "master":
		return m.teamOperation(confirm, u.MemberIndex, false)
	default:
		return nil
	}
}

func (m *Model) teamKey(v tea.KeyMsg) tea.Cmd {
	u := m.teams
	k := v.String()
	if handled, cmd := m.transferKey(v); handled {
		return cmd
	}
	if k == "q" && u.Screen == "picker" {
		return tea.Quit
	}
	if u.Confirm != "" {
		if k == "y" {
			return m.teamAction("team-yes")
		}
		return m.teamAction("team-no")
	}
	if k == "esc" {
		return m.teamEscape()
	}
	if u.Screen == "team-form" || u.Screen == "member-form" {
		return m.formKey(v)
	}
	if k == "up" || k == "down" {
		m.teamArrow(k)
		return nil
	}
	return m.teamShortcut(k)
}

func (m *Model) teamEscape() tea.Cmd {
	u := m.teams
	if u.Screen == "choices" {
		u.Screen = u.formScreen()
		return nil
	}
	if u.Screen == "team-form" || u.Screen == "member-form" {
		return m.teamAction("form-cancel")
	}
	if u.Screen == "members" {
		u.Screen = ""
		return m.loadDashboard()
	}
	return nil
}

func (m *Model) formKey(v tea.KeyMsg) tea.Cmd {
	k := v.String()
	switch k {
	case "tab":
		return m.teamAction("form-next")
	case "shift+tab":
		return m.teamAction("form-prev")
	case "ctrl+s":
		return m.teamAction("form-save")
	case "ctrl+o":
		return m.teamAction("form-choice")
	case "ctrl+l":
		return m.teamAction("form-catalog")
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(v)
		return cmd
	}
}

func (m *Model) teamArrow(k string) {
	u := m.teams
	if k == "up" {
		if u.Screen == "members" {
			u.MemberIndex = max(0, u.MemberIndex-1)
		} else {
			u.Index = max(0, u.Index-1)
		}
		return
	}
	if k == "down" {
		switch u.Screen {
		case "members":
			u.MemberIndex = min(len(u.Manager.Team.Members)-1, u.MemberIndex+1)
		case "choices":
			u.Index = min(len(u.Fields[u.Field].Choices)-1, u.Index+1)
		default:
			u.Index = min(max(0, len(u.Teams)-1), u.Index+1)
		}
		return
	}
}

func (m *Model) teamShortcut(k string) tea.Cmd {
	u := m.teams
	if k == "enter" {
		switch u.Screen {
		case "picker":
			return m.teamAction("team-open")
		case "choices":
			return m.teamAction(fmt.Sprintf("choice:%d", u.Index))
		case "members":
			return m.teamAction("member-edit")
		}
	}
	if k == "n" && u.Screen == "picker" {
		return m.teamAction("team-new")
	}
	if u.Screen == "members" {
		action := map[string]string{
			"n": "team-add", "s": "member-start", "d": "member-remove",
			"m": "member-master", "e": "team-settings", "t": "team-picker",
		}[k]
		return m.teamAction(action)
	}
	if u.Screen == "picker" {
		action := map[string]string{
			"i": "team-import", "e": "team-export", "r": "team-list", "d": "team-delete",
			"c": "team-duplicate", "ctrl+e": "team-edit", "a": "team-all",
		}[k]
		return m.teamAction(action)
	}
	return nil
}

func (m *Model) handleTeams(msg tea.Msg) (bool, tea.Cmd) {
	u := m.teams
	if u == nil {
		return false, nil
	}
	switch v := msg.(type) {
	case liveImportMsg:
		u.Busy = false
		if v.Err != nil {
			m.note = v.Err.Error()
			return true, nil
		}
		u.Transfer.Live = v.Sessions
		u.Screen = "import-live"
		u.Index = 0
		return true, nil
	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return true, tea.Quit
		}
		if u.Screen != "" || u.Confirm != "" {
			return true, m.teamKey(v)
		}
	case teamsMsg:
		return true, m.applyTeams(v)
	case catalogMsg:
		m.applyCatalog(v)
		return true, nil
	case teamOperationMsg:
		u.Busy = false
		u.Manager = v.Manager
		if v.Err != nil {
			m.note = v.Err.Error()
		} else {
			m.note = "Team operation completed"
		}
		u.Screen = ""
		return true, m.loadDashboard()
	case teamReloadMsg:
		if v.Err != nil {
			m.note = "Team reload: " + v.Err.Error()
			return true, nil
		}
		if u.Screen != "" {
			m.note = "Team changed on disk; close editor to reload"
			return true, nil
		}
		u.Manager.Team = v.Team
		u.Manager.Version = v.Version
		return true, m.loadDashboard()
	}
	return false, nil
}

func (m *Model) applyTeams(v teamsMsg) tea.Cmd {
	u := m.teams
	if v.Err != nil {
		m.note = v.Err.Error()
		return nil
	}
	u.Teams, u.Summary, u.Status = v.Teams, v.Summary, v.Status
	if u.Screen == "picker" {
		u.Index = min(u.Index, max(0, len(u.Teams)-1))
	}
	if v.Status == nil {
		return m.teamStatuses(v.Teams)
	}
	return nil
}

func (m *Model) applyCatalog(v catalogMsg) {
	u := m.teams
	if v.Err != nil {
		m.note = v.Err.Error()
		return
	}
	u.Models, u.Agents = v.Models, v.Agents
	for i := range u.Fields {
		if strings.Contains(u.Fields[i].Label, "model") || u.Fields[i].Label == "Model" {
			u.Fields[i].Choices = v.Models
		}
		if u.Fields[i].Label == "OpenCode agent" {
			u.Fields[i].Choices = v.Agents
		}
	}
}
