package ui

import (
	"slices"

	"github.com/vqivs/agent-deck/internal/team"
)

func kanbanColumnWidths(tasks []team.Task, screen *KanbanScreen, width int) (firstColumn int, columnWidths []int) {
	states := screen.states()
	selected := max(0, slices.Index(states, screen.Column))
	minimum := 30
	if width >= 154 {
		minimum = min(22, max(16, (width-12)/max(1, len(states)-1)))
	}
	widths := make([]int, len(states))
	total := 0
	for i, state := range states {
		widths[i] = minimum
		if len(kanbanTasks(tasks, screen.Filter, state, false)) == 0 {
			widths[i] = min(12, len(state)+5)
		}
		total += widths[i]
	}
	if total <= width {
		kanbanDistribute(widths, width-total)
		return 0, widths
	}
	first := max(0, min(selected-1, len(states)-1))
	for first < selected && widths[first]+widths[selected] > width {
		first++
	}
	end, total := first, 0
	for end < len(states) && total+widths[end] <= width {
		total += widths[end]
		end++
	}
	if end == first {
		return first, []int{width}
	}
	visible := slices.Clone(widths[first:end])
	kanbanDistribute(visible, width-total)
	return first, visible
}

func kanbanDistribute(widths []int, extra int) {
	for extra > 0 {
		changed := false
		for i := range widths {
			if widths[i] > 12 && extra > 0 {
				widths[i]++
				extra--
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}
