package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestToolCardGoldens(t *testing.T) {
	for _, tc := range []struct{ tool, input, want string }{
		{"bash", `{"command":"go test ./..."}`, "$ go test ./..."},
		{"bash", `{"command":"python3 - <<PY\nprint(1)\nPY"}`, "$ python3 - <<PY … (3 lines)"},
		{"read", `{"filePath":"/project/app/Video.php","offset":1,"limit":120}`, "📄 read app/Video.php:1-120"},
		{"edit", `{"filePath":"/project/helpers.php","oldString":"a\nb","newString":"c"}`, "✏ edit helpers.php (+1 −2)"},
		{"apply_patch", `{"patchText":"*** Update File: /project/a.go\n-old\n+new\n+more"}`, "✏ patch a.go (+2 −1)"},
		{"grep", `{"pattern":"AR\\Bon","path":"/project/app"}`, `🔍 grep "AR\\Bon" in app`},
		{"glob", `{"pattern":"**/*.go","path":"/project"}`, "🔍 glob **/*.go in ."},
		{"webfetch", `{"url":"https://example.org"}`, "🌐 fetch https://example.org"},
		{"task", `{"description":"Check tests"}`, `🧩 task "Check tests"`},
		{"future", `{"query":"something","secret":"not displayed"}`, "⚙ future something"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			p := api.Part{Tool: tc.tool}
			p.State.Input = json.RawMessage(tc.input)
			if got := Card(&p, "/project"); got != tc.want {
				t.Fatalf("golden mismatch\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestMarkdownWidthAndExpansion(t *testing.T) {
	r := New()
	for _, dark := range []bool{false, true} {
		for _, width := range []int{20, 40, 80} {
			out := r.Markdown(
				"# Heading\n\n**bold** and `inline`\n\n- item\n\n| A | B |\n|---|---|\n| one | two |\n\n"+
					"```go\nfmt.Println(\"hello\")\n```",
				width,
				dark,
			)
			if lipgloss.Width(out) > width || !strings.Contains(ansi.Strip(out), "Heading") {
				t.Fatalf("width %d got %d", width, lipgloss.Width(out))
			}
		}
	}
	p := api.Part{ID: "tool", Type: "tool", Tool: "bash"}
	p.State.Status = "completed"
	p.State.Input = json.RawMessage(`{"command":"go test"}`)
	p.State.Output = "first\nsecond\nthird\nfourth\n\x1b[31mlast\x1b[0m"
	o := Options{Width: 80, Full: true}
	collapsed, _ := r.Render([]api.Part{p}, o)
	if strings.Contains(collapsed, "first") || strings.Contains(collapsed, `"command"`) {
		t.Fatal(collapsed)
	}
	o.Expanded = map[string]bool{"tool": true}
	expanded, _ := r.Render([]api.Part{p}, o)
	if !strings.Contains(expanded, "first") || !strings.Contains(expanded, "[raw]") {
		t.Fatal(expanded)
	}
	o.Raw = map[string]bool{"tool": true}
	raw, _ := r.Render([]api.Part{p}, o)
	if !strings.Contains(raw, `"command"`) {
		t.Fatal(raw)
	}
	text, _ := r.Render([]api.Part{{ID: "reason", Type: "reasoning", Text: "hidden thought"}}, Options{Width: 80})
	if text != "" {
		t.Fatal("reasoning visible")
	}
}
