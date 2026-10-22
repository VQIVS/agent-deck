package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) toggleCard(i int, id string) {
	p := &m.panes[i]
	if p.Expanded == nil {
		p.Expanded = map[string]bool{}
	}
	p.Expanded[id] = !p.Expanded[id]
}

func (m *Model) toggleRaw() {
	if len(m.panes) == 0 {
		return
	}
	p := &m.panes[m.focus]
	if p.Raw == nil {
		p.Raw = map[string]bool{}
	}
	p.Raw[p.Selected] = !p.Raw[p.Selected]
}

func (m *Model) selectCard(next bool) {
	if len(m.panes) == 0 {
		return
	}
	p := &m.panes[m.focus]
	var ids []string
	for i := range p.State.Parts {
		part := &p.State.Parts[i]
		if part.Type == "tool" || part.Role == "user" {
			ids = append(ids, part.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	pos := -1
	for i, id := range ids {
		if id == p.Selected {
			pos = i
		}
	}
	delta := 1
	if !next {
		delta = -1
	}
	p.Selected = ids[(pos+delta+len(ids))%len(ids)]
}

func (m *Model) find() {
	if len(m.panes) == 0 {
		return
	}
	q := strings.ToLower(m.input.Value())
	if q == "" {
		return
	}
	p := &m.panes[m.focus]
	for i, line := range strings.Split(ansi.Strip(p.Content), "\n") {
		if strings.Contains(strings.ToLower(line), q) {
			p.View.SetYOffset(i)
			return
		}
	}
}
