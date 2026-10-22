package transcript

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

func relative(path, dir string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if r, e := filepath.Rel(dir, path); e == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return path
}

func Card(p *api.Part, dir string) string {
	var args map[string]any
	_ = json.Unmarshal(p.State.Input, &args)
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := args[k].(string); ok {
				return Clean(s)
			}
		}
		return ""
	}
	num := func(k string) int { n, _ := args[k].(float64); return int(n) }
	path := relative(str("filePath", "path", "file_path"), dir)
	switch p.Tool {
	case "bash":
		lines := strings.Split(strings.TrimSpace(str("command")), "\n")
		text := "$ " + lines[0]
		if len(lines) > 1 {
			text += fmt.Sprintf(" … (%d lines)", len(lines))
		}
		return text
	case "read":
		text := "📄 read " + path
		start := num("offset")
		if start < 1 {
			start = 1
		}
		if limit := num("limit"); limit > 0 {
			text += fmt.Sprintf(":%d-%d", start, start+limit-1)
		}
		return text
	case "edit":
		old, replacement := str("oldString", "old_string"), str("newString", "new_string")
		return fmt.Sprintf("✏ edit %s (+%d −%d)", path, lineCount(replacement), lineCount(old))
	case "apply_patch":
		return patchCard(str("patchText", "patch"), dir)
	case "grep":
		return "🔍 grep " + fmt.Sprintf("%q", str("pattern")) + " in " + relative(str("path"), dir)
	case "glob":
		return "🔍 glob " + str("pattern") + " in " + relative(str("path"), dir)
	case "webfetch":
		return "🌐 fetch " + str("url")
	case "task":
		return "🧩 task " + fmt.Sprintf("%q", str("description"))
	default:
		arg := str("description", "command", "query", "pattern", "url", "filePath", "path", "name")
		return "⚙ " + Clean(p.Tool) + " " + relative(arg, dir)
	}
}

func patchCard(patch, dir string) string {
	var files []string
	plus, minus := 0, 0
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
			if strings.HasPrefix(line, prefix) {
				files = append(files, relative(strings.TrimPrefix(line, prefix), dir))
			}
		}
		if strings.HasPrefix(line, "+") {
			plus++
		}
		if strings.HasPrefix(line, "-") {
			minus++
		}
	}
	return fmt.Sprintf("✏ patch %s (+%d −%d)", strings.Join(files, ", "), plus, minus)
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(s, "\n"), "\n"))
}

// Range identifies the rendered lines occupied by a tool card.
type Range struct {
	ID         string
	Start, End int
}

// Options controls transcript width, theme, visibility, and card expansion.
type Options struct {
	Width                 int
	Dir                   string
	Dark, Full, Reasoning bool
	Expanded, Raw         map[string]bool
	Selected              string
	Frame                 int
}

// Renderer caches Markdown renderers for bounded terminal widths and themes.
type Renderer struct {
	cache    map[string]string
	markdown map[string]*glamour.TermRenderer
}

func New() *Renderer {
	return &Renderer{cache: map[string]string{}, markdown: map[string]*glamour.TermRenderer{}}
}

func (r *Renderer) Markdown(text string, width int, dark bool) string {
	width = max(8, width)
	style := "light"
	if dark {
		style = "dark"
	}
	key := fmt.Sprintf("%d/%s", width, style)
	cacheKey := key + "/" + text
	if s, ok := r.cache[cacheKey]; ok {
		return s
	}
	renderer := r.markdown[key]
	if renderer == nil {
		renderer, _ = glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(max(4, width-4)))
		r.markdown[key] = renderer
	}
	result := Clean(text)
	if renderer != nil {
		if s, e := renderer.Render(Clean(text)); e == nil {
			result = strings.TrimSpace(s)
		}
	}
	result = ansi.Hardwrap(result, width, true)
	if len(r.cache) > 512 {
		r.cache = map[string]string{}
	}
	r.cache[cacheKey] = result
	return result
}

func (r *Renderer) Render(parts []api.Part, o Options) (string, []Range) {
	w := max(8, o.Width)
	var blocks []string
	var ranges []Range
	line := 0
	appendBlock := func(s, id string) {
		if s == "" {
			return
		}
		s = ansi.Hardwrap(s, w, true)
		n := strings.Count(s, "\n") + 1
		ranges = append(ranges, Range{id, line, line + n})
		blocks = append(blocks, s)
		line += n + 1
	}
	start := 0
	if !o.Full && len(parts) > 7 {
		start = len(parts) - 7
	}
	for i := start; i < len(parts); i++ {
		p := &parts[i]
		if p.Role == "user" {
			stamp := ""
			if p.Created > 0 {
				stamp = time.UnixMilli(p.Created).Format("15:04:05")
			}
			appendBlock("── turn "+stamp+" ──", "")
		}
		if p.Type == "reasoning" && !o.Reasoning {
			continue
		}
		if p.Type != "tool" {
			appendBlock(r.textPart(p, o, w), p.ID)
			continue
		}
		appendBlock(toolPart(p, o, w), p.ID)
	}
	return strings.Join(blocks, "\n\n"), ranges
}

func (r *Renderer) textPart(p *api.Part, o Options, width int) string {
	text := p.Text
	if p.Role == "user" && !o.Expanded[p.ID] {
		text = ansi.Truncate(strings.ReplaceAll(Clean(text), "\n", " "), max(8, width-10), "…")
	}
	text = r.Markdown(text, width, o.Dark)
	if p.Role == "user" || p.Type == "reasoning" {
		text = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(ansi.Strip(text))
	}
	return text
}

func toolPart(p *api.Part, o Options, width int) string {
	marker, color := toolMarker(p.State.Status, o.Frame)
	prefix := "▸ "
	if o.Expanded[p.ID] {
		prefix = "▾ "
	}
	if p.ID == o.Selected {
		prefix = "> "
	}
	card := prefix + Card(p, o.Dir) + " " + marker + toolDuration(p)
	card = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(ansi.Truncate(card, width, "…"))
	if p.Tool == "bash" {
		var metadata map[string]any
		_ = json.Unmarshal(p.State.Metadata, &metadata)
		if code, ok := metadata["exit"]; ok {
			card += fmt.Sprintf(" · exit %v", code)
		}
	}
	output := toolOutput(p, o.Expanded[p.ID])
	if output != "" {
		card += "\n" + ansi.Hardwrap(output, width, true)
	}
	if o.Expanded[p.ID] {
		card += "\n[raw]"
		if o.Raw[p.ID] {
			b, _ := json.MarshalIndent(p, "", "  ")
			card += "\n" + Clean(string(b))
		}
	}
	return card
}

func toolMarker(status string, frame int) (marker, color string) {
	switch status {
	case "running", "pending":
		return []string{"⠋", "⠙", "⠹", "⠸"}[frame%4], "214"
	case "error":
		return "✗", "196"
	default:
		return "✓", "42"
	}
}

func toolDuration(p *api.Part) string {
	if p.State.Time.Start <= 0 {
		return ""
	}
	end := p.State.Time.End
	if end == 0 {
		end = time.Now().UnixMilli()
	}
	return " " + (time.Duration(max(0, end-p.State.Time.Start)) * time.Millisecond).String()
}

func toolOutput(p *api.Part, expanded bool) string {
	output := Clean(p.State.Output)
	if p.State.Error != "" {
		output = Clean(p.State.Error)
	}
	if !expanded {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		output = strings.Join(lines, "\n")
		if p.Tool != "bash" && p.State.Error == "" {
			output = ""
		}
	}
	if expanded && (p.Tool == "edit" || p.Tool == "apply_patch") {
		return patchOutput(p)
	}
	return output
}

func patchOutput(p *api.Part) string {
	var a map[string]string
	_ = json.Unmarshal(p.State.Input, &a)
	diff := a["patchText"]
	if diff == "" {
		diff = a["patch"]
	}
	if diff == "" {
		for _, s := range strings.Split(a["oldString"], "\n") {
			diff += "-" + s + "\n"
		}
		for _, s := range strings.Split(a["newString"], "\n") {
			diff += "+" + s + "\n"
		}
	}
	parts := strings.Split(Clean(diff), "\n")
	lines := make([]string, 0, len(parts))
	for _, s := range parts {
		color := "245"
		if strings.HasPrefix(s, "+") {
			color = "42"
		}
		if strings.HasPrefix(s, "-") {
			color = "196"
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s))
	}
	return strings.Join(lines, "\n")
}
