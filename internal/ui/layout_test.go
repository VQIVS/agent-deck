package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/config"
	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestGrid(t *testing.T) {
	for _, lead := range []bool{false, true} {
		for _, w := range []int{20, 44, 80, 132, 240} {
			for _, h := range []int{8, 16, 40} {
				capacity := Capacity(w, h, lead)
				for n := 1; n <= min(7, capacity); n++ {
					rects := Grid(w, h, n, lead)
					if len(rects) != n {
						t.Fatal("missing panes")
					}
					area := 0
					for i, r := range rects {
						if r.W < 1 || r.H < 1 || r.X < 0 || r.Y < 0 || r.X+r.W > w || r.Y+r.H > h {
							t.Fatalf("out of bounds %+v", r)
						}
						area += r.W * r.H
						for j := 0; j < i; j++ {
							s := rects[j]
							if r.X < s.X+s.W && s.X < r.X+r.W && r.Y < s.Y+s.H && s.Y < r.Y+r.H {
								t.Fatal("overlap")
							}
						}
					}
					if area != w*h {
						t.Fatalf("unfilled grid %d != %d", area, w*h)
					}
				}
			}
		}
	}
}

func TestPagingAndView(t *testing.T) {
	d := config.Deck{Server: "http://localhost"}
	for i := 0; i < 31; i++ {
		d.Sessions = append(
			d.Sessions,
			config.Agent{Name: fmt.Sprint(i), Session: fmt.Sprintf("session-%d", i), Dir: "/tmp/project"},
		)
	}
	m := New(context.Background(), &d, Options{NoGit: true})
	for i := range m.panes {
		m.panes[i].State.Upsert(&api.Part{ID: "p", Type: "text", Text: strings.Repeat("wide text 界\n", 200)})
	}
	m.w = 100
	m.h = 35
	for i := range m.panes {
		m.focus = i
		indices, r, _, pages := m.page(32)
		if pages < 2 || len(indices) != len(r) {
			t.Fatal("bad pages")
		}
		found := false
		for _, idx := range indices {
			if idx == i {
				found = true
			}
		}
		if !found {
			t.Fatal("focus off page")
		}
		view := m.View()
		if lipgloss.Width(view) > 100 || lipgloss.Height(view) > 35 {
			t.Fatalf("overflow %dx%d", lipgloss.Width(view), lipgloss.Height(view))
		}
	}
	m.hidden["session-0"] = true
	indices, _, _, _ := m.page(32)
	for _, i := range indices {
		if i == 0 {
			t.Fatal("hidden visible")
		}
	}
}

func TestGroupingAndFuzzy(t *testing.T) {
	in := []config.Agent{
		{Session: "session-c", Parent: "session-p"},
		{Session: "session-p"},
		{Session: "session-other"},
	}
	out := grouped(in)
	if out[0].Session != "session-p" || out[1].Session != "session-c" {
		t.Fatal(out)
	}
	if !fuzzy("tst", "Tests worker") || fuzzy("zzz", "Tests") {
		t.Fatal("filter")
	}
	// Malformed parent cycles must not recurse forever.
	if len(grouped([]config.Agent{{Session: "a", Parent: "b"}, {Session: "b", Parent: "a"}})) != 2 {
		t.Fatal("cycle lost")
	}
}

func TestHiddenNavigationAndReadOnly(t *testing.T) {
	d := config.Deck{
		Server: "http://localhost",
		Sessions: []config.Agent{
			{Name: "Parent", Session: "session-p", Dir: "/tmp"},
			{Name: "Child", Session: "session-c", Dir: "/tmp", Parent: "session-p"},
			{Name: "Other", Session: "session-o", Dir: "/tmp"},
		},
	}
	m := New(context.Background(), &d, Options{ReadOnly: true, NoGit: true})
	m.hidden["session-c"] = true
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 2 {
		t.Fatal("focused hidden pane")
	}
	for _, key := range []rune{'m', 'n', 's', 'a'} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if m.editing || m.confirm {
			t.Fatal("read-only guard")
		}
		if cmd != nil {
			t.Fatal("read-only command")
		}
	}
}
