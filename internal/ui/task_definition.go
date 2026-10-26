package ui

import (
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

// TaskDefinitionDetail displays explicitly supplied merged requirements separately from progress claims.
func TaskDefinitionDetail(definition team.DefinitionOfDone, width int) string {
	if len(definition) == 0 || width <= 0 {
		return ""
	}
	lines := make([]string, 0, len(definition)+2)
	lines = append(lines, "Merged definition of done (task + current team)")
	for _, criterion := range definition {
		label := "optional"
		if criterion.Required {
			label = "required"
		}
		lines = append(lines, criterion.ID+" ["+label+"]: "+criterion.Text)
	}
	lines = append(lines, "Requirements only; not evidence of completion.")
	for i := range lines {
		lines[i] = RenderInboxLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
