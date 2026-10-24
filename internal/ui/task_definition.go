package ui

import (
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

func ParseTaskDefinition(text string) (team.DefinitionOfDone, error) {
	return ParseTeamDoD(text)
}

func SerializeTaskDefinition(definition team.DefinitionOfDone) (string, error) {
	return FormatTeamDoD(definition)
}

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

func TaskEvidenceWithDefinition(task *team.Task, observation *TaskGitObservation,
	target *TaskEvidenceTarget, definition team.DefinitionOfDone, width int,
) string {
	view := TaskEvidenceDetail(task, observation, target, width)
	if requirements := TaskDefinitionDetail(definition, width); requirements != "" {
		if view != "" {
			view += "\n\n"
		}
		view += requirements
	}
	return view
}
