package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

type transferUI struct {
	Path, Name, Repo, Server, Format string
	Portable                         bool
	Draft                            team.Team
	Old                              []byte
	Preview                          string
	Scroll                           int
	Live                             []api.Session
	Selected                         map[int]bool
	Master                           int
}
type liveImportMsg struct {
	Sessions []api.Session
	Err      error
}

func (m *Model) transferView() string {
	u := m.teams
	x := &u.Transfer
	if u.Screen == "import-preview" {
		lines := strings.Split(ansi.Hardwrap(clean(x.Preview), max(1, m.w), true), "\n")
		height := max(1, m.h-12)
		x.Scroll = min(x.Scroll, max(0, len(lines)-height))
		title := "Import preview · new team"
		if len(x.Old) > 0 {
			title = "Import preview · REPLACE existing team"
		}
		return m.theme.title(
			title,
		) + "\n" + m.theme.dim(
			"↑↓ / PgUp PgDn scroll · Ctrl+S confirm save",
		) + "\n" + strings.Join(
			lines[x.Scroll:min(len(lines), x.Scroll+height)],
			"\n",
		) + "\n" + m.buttonRow(
			[]button{{"Confirm save", "import-save", m.opts.ReadOnly}, {"Cancel", "transfer-cancel", false}},
			0,
			2+min(height, len(lines)),
			m.w,
			-1,
		) + "\n" + m.theme.dim(
			fit(m.note, m.w),
		)
	}
	if u.Screen == "import-live" {
		return m.liveImportView()
	}
	return m.transferFormView()
}

func (m *Model) liveImportView() string {
	u := m.teams
	x := &u.Transfer
	var b strings.Builder
	b.WriteString(
		m.theme.title(
			"Import from live sessions",
		) + "\n" + m.theme.dim(
			"↑↓ select · Space include · m master · Enter preview · Esc back",
		) + "\n",
	)
	height := max(1, m.h-13)
	start := max(0, u.Index-height+1)
	for i := start; i < min(len(x.Live), start+height); i++ {
		s := x.Live[i]
		marker := "[ ]"
		if x.Selected[i] {
			marker = "[✓]"
		}
		if x.Master == i {
			marker = "[★]"
		}
		prefix := "  "
		if i == u.Index {
			prefix = "› "
		}
		b.WriteString(fit(prefix+marker+" "+s.Title+" · "+s.Directory, m.w) + "\n")
		m.zones = append(
			m.zones,
			zone{Rect: Rect{0, 2 + i - start, m.w, 1}, Action: fmt.Sprintf("live-row:%d", i), Pane: -1},
		)
	}
	y := 2 + min(len(x.Live), height)
	if len(x.Live) == 0 {
		b.WriteString(m.theme.dim("No sessions found. Check the server URL and try again.") + "\n")
		y++
	}
	b.WriteString(
		m.buttonRow(
			[]button{
				{"Include / exclude", "live-toggle", false},
				{"Set master", "live-master", false},
				{"Preview", "live-preview", m.opts.ReadOnly},
				{"Back", "transfer-cancel", false},
			},
			0,
			y,
			m.w,
			-1,
		) + "\n" + m.theme.dim(
			fit(m.note, m.w),
		),
	)
	return b.String()
}

func (m *Model) transferFormView() string {
	u := m.teams
	x := &u.Transfer
	title := "Import a team"
	if u.Screen == "export" {
		title = "Export team"
	}
	var b strings.Builder
	b.WriteString(
		m.theme.title(title) + "\n" + m.theme.dim("Tab next field · Ctrl+S preview / export · Esc cancel") + "\n",
	)
	f := u.Fields[u.Field]
	b.WriteString(m.theme.title(f.Label) + "\n")
	m.input.SetWidth(max(1, m.w-2))
	m.input.SetHeight(2)
	b.WriteString(m.input.View() + "\n")
	m.zones = append(m.zones, zone{Rect: Rect{0, 3, m.w, 2}, Action: "input", Pane: -1})
	buttons := []button{
		{"Previous", "form-prev", false},
		{"Next", "form-next", false},
		{"Preview file", "import-file", m.opts.ReadOnly},
		{"From live sessions", "import-discover", m.opts.ReadOnly},
		{"Cancel", "transfer-cancel", false},
	}
	if u.Screen == "export" {
		buttons = []button{
			{"Previous", "form-prev", false},
			{"Next", "form-next", false},
			{fmt.Sprintf("Portable: %t", x.Portable), "export-portable", false},
			{"Export", "export-save", m.opts.ReadOnly},
			{"Cancel", "transfer-cancel", false},
		}
	}
	b.WriteString(m.buttonRow(buttons, 0, 5, m.w, -1) + "\n")
	if u.Screen == "export" {
		b.WriteString(m.theme.dim("Portable removes session IDs and machine paths; bind --repo on import.") + "\n")
	} else {
		b.WriteString(m.theme.dim("Legacy YAML/JSON, exported teams, or existing live sessions.") + "\n")
	}
	b.WriteString(m.theme.dim(fit(m.note, m.w)))
	return b.String()
}

func (m *Model) transferValues() {
	u := m.teams
	x := &u.Transfer
	if len(u.Fields) > 0 {
		u.Fields[u.Field].Value = m.input.Value()
	}
	for _, f := range u.Fields {
		switch f.Label {
		case "File path":
			x.Path = f.Value
		case "Team name":
			x.Name = f.Value
		case "Local repository (portable templates)":
			x.Repo = f.Value
		case "Server URL":
			x.Server = f.Value
		case "Output file":
			x.Path = f.Value
		case "Format (yaml/json)":
			x.Format = f.Value
		}
	}
}

func (m *Model) previewImport(t *team.Team) {
	u := m.teams
	if err := t.Validate(); err != nil {
		m.note = err.Error()
		return
	}
	old, err := os.ReadFile(u.Store.Path(t.Name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.note = err.Error()
		return
	}
	u.Transfer.Draft = *t
	u.Transfer.Old = old
	u.Transfer.Preview = team.Preview(t, old)
	u.Transfer.Scroll = 0
	u.Screen = "import-preview"
}

func (m *Model) transferAction(action string) (bool, tea.Cmd) {
	u := m.teams
	x := &u.Transfer
	if strings.HasPrefix(action, "live-row:") {
		_, _ = fmt.Sscanf(action, "live-row:%d", &u.Index)
		return true, nil
	}
	switch action {
	case "team-import", "team-export":
		m.openTransfer(action)
		return true, nil
	case "transfer-cancel":
		u.Screen = "picker"
		u.Confirm = ""
		m.note = ""
		return true, nil
	case "import-file":
		m.importFile()
		return true, nil
	case "import-discover":
		return true, m.discoverImport()
	case "live-toggle":
		x.toggle(u.Index)
		return true, nil
	case "live-master":
		if len(x.Live) > 0 {
			x.Master = u.Index
			x.Selected[u.Index] = true
		}
		return true, nil
	case "live-preview":
		m.previewLive()
		return true, nil
	case "import-save":
		return true, m.saveImport()
	case "export-portable":
		x.Portable = !x.Portable
		return true, nil
	case "export-save":
		m.saveExport()
		return true, nil
	case "team-yes":
		if u.Confirm == "export-replace" {
			return m.transferAction("export-save")
		}
	}
	return false, nil
}

func (x *transferUI) toggle(index int) {
	if len(x.Live) == 0 {
		return
	}
	x.Selected[index] = !x.Selected[index]
	if !x.Selected[index] && x.Master == index {
		x.Master = -1
	}
}

func (m *Model) openTransfer(action string) {
	u := m.teams
	x := &u.Transfer
	if m.opts.ReadOnly {
		m.note = "Read-only: transfers disabled"
		return
	}
	*x = transferUI{}
	x.Master = -1
	x.Selected = map[int]bool{}
	x.Format = "yaml"
	x.Server = m.deck.Server
	if x.Server == "" {
		x.Server = "http://127.0.0.1:4096"
	}
	u.Screen = "import-form"
	u.Fields = []formField{
		{Label: "File path"},
		{Label: "Team name"},
		{Label: "Local repository (portable templates)"},
		{Label: "Server URL", Value: x.Server},
	}
	if action == "team-export" {
		if len(u.Teams) == 0 {
			return
		}
		x.Draft = u.Teams[u.Index]
		u.Screen = "export"
		u.Fields = []formField{
			{Label: "Output file", Value: team.Slug(x.Draft.Name) + ".yaml"},
			{Label: "Format (yaml/json)", Value: "yaml"},
		}
	}
	u.Field = 0
	m.input.SetValue(u.Fields[0].Value)
	m.input.Focus()
}

func (m *Model) importFile() {
	x := &m.teams.Transfer
	if m.opts.ReadOnly {
		return
	}
	m.transferValues()
	b, err := os.ReadFile(x.Path)
	if err != nil {
		m.note = err.Error()
		return
	}
	name := x.Name
	t, err := team.Import(b, team.ImportOptions{Name: name, Repo: x.Repo})
	if err != nil && name == "" {
		name = strings.TrimSuffix(filepath.Base(x.Path), filepath.Ext(x.Path))
		t, err = team.Import(b, team.ImportOptions{Name: name, Repo: x.Repo})
	}
	if err != nil {
		m.note = err.Error()
		return
	}
	m.previewImport(&t)
}

func (m *Model) discoverImport() tea.Cmd {
	u := m.teams
	x := &u.Transfer
	if m.opts.ReadOnly || u.Busy {
		return nil
	}
	m.transferValues()
	if strings.TrimSpace(x.Name) == "" {
		m.note = "Enter a team name, then Ctrl+L to choose live sessions."
		return nil
	}
	u.Busy = true
	server := x.Server
	ctx := u.RootContext()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		v, err := api.New(server, true).Discover(ctx)
		return liveImportMsg{v.Sessions, err}
	}
}

func (m *Model) previewLive() {
	x := &m.teams.Transfer
	if x.Master < 0 || !x.Selected[x.Master] {
		m.note = "Choose exactly one master with m or Set master."
		return
	}
	imported := team.Team{Version: 1, Name: x.Name, Server: x.Server, Base: "main"}
	for i, s := range x.Live {
		if !x.Selected[i] {
			continue
		}
		name := s.Title
		if name == "" {
			name = s.ID
		}
		member := team.Member{
			Name: name, Session: s.ID, Dir: s.Directory, Role: "worker",
			Mode: "adopt", Capability: "inherited",
		}
		if i == x.Master {
			member.Role, imported.Repo = "master", s.Directory
		}
		imported.Members = append(imported.Members, member)
	}
	imported.Capacity = max(1, len(imported.Members)-1)
	m.previewImport(&imported)
}

func (m *Model) saveImport() tea.Cmd {
	u := m.teams
	x := &u.Transfer
	if m.opts.ReadOnly {
		return nil
	}
	if _, err := u.Store.Save(m.context(), &x.Draft, x.Old); err != nil {
		m.note = err.Error()
		return nil
	}
	m.note = "Imported " + x.Draft.Name + " · existing sessions adopted without messages"
	u.Screen = "picker"
	return m.listTeams()
}

func (m *Model) saveExport() {
	u := m.teams
	x := &u.Transfer
	if m.opts.ReadOnly {
		return
	}
	m.transferValues()
	b, err := team.Export(&x.Draft, x.Format, x.Portable)
	if err != nil {
		m.note = err.Error()
		return
	}
	if _, err = os.Stat(x.Path); err == nil && u.Confirm != "export-replace" {
		u.Confirm = "export-replace"
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.note = err.Error()
		return
	}
	if err = team.WriteExport(m.context(), x.Path, b, u.Confirm == "export-replace"); err != nil {
		m.note = err.Error()
		u.Confirm = ""
		return
	}
	u.Confirm = ""
	u.Screen = "picker"
	m.note = "Exported " + x.Path
}

func (m *Model) transferKey(v tea.KeyMsg) (bool, tea.Cmd) {
	u := m.teams
	k := v.String()
	if u.Confirm != "" {
		return false, nil
	}
	if !strings.HasPrefix(u.Screen, "import") && u.Screen != "export" {
		return false, nil
	}
	if k == "esc" {
		return m.transferAction("transfer-cancel")
	}
	if u.Screen == "import-preview" {
		return m.previewKey(k)
	}
	if u.Screen == "import-live" {
		return m.liveKey(k)
	}
	return m.transferFormKey(v)
}

func (m *Model) previewKey(k string) (bool, tea.Cmd) {
	x := &m.teams.Transfer
	switch k {
	case "up":
		x.Scroll = max(0, x.Scroll-1)
	case "down":
		x.Scroll++
	case "pgup":
		x.Scroll = max(0, x.Scroll-10)
	case "pgdown":
		x.Scroll += 10
	case "ctrl+s":
		return m.transferAction("import-save")
	}
	return true, nil
}

func (m *Model) liveKey(k string) (bool, tea.Cmd) {
	u := m.teams
	x := &u.Transfer
	switch k {
	case "up":
		u.Index = max(0, u.Index-1)
	case "down":
		u.Index = min(max(0, len(x.Live)-1), u.Index+1)
	case " ":
		return m.transferAction("live-toggle")
	case "m":
		return m.transferAction("live-master")
	case "enter":
		return m.transferAction("live-preview")
	}
	return true, nil
}

func (m *Model) transferFormKey(v tea.KeyMsg) (bool, tea.Cmd) {
	u := m.teams
	k := v.String()
	switch k {
	case "tab":
		return true, m.teamAction("form-next")
	case "shift+tab":
		return true, m.teamAction("form-prev")
	case "ctrl+l":
		return m.transferAction("import-discover")
	case "ctrl+p":
		if u.Screen == "export" {
			return m.transferAction("export-portable")
		}
	case "ctrl+s":
		if u.Screen == "export" {
			return m.transferAction("export-save")
		}
		return m.transferAction("import-file")
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(v)
	return true, cmd
}
