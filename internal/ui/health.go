package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (t *Theme) StateBadge(status string) string {
	color := t.Muted
	switch status {
	case "BUSY":
		color = t.Warning
	case "WAITING":
		color = t.Waiting
	case "STALLED", "ERROR":
		color = t.Danger
	case "IDLE", "DONE":
		color = t.Success
	}
	return t.chip(status, color)
}

// ContextGauge uses the selected model's context limit; zero means unknown.
func (t *Theme) ContextGauge(tokens, limit int64) string {
	tokens = max(0, tokens)
	if limit <= 0 {
		return t.dim(fmt.Sprintf("context %d / unknown", tokens))
	}
	ratio := float64(tokens) / float64(limit)
	color := t.Success
	if ratio >= 0.9 {
		color = t.Danger
	} else if ratio >= 0.75 {
		color = t.Warning
	}
	filled := int(min(1.0, ratio) * 10)
	text := fmt.Sprintf("context [%s%s] %d/%d (%.0f%%)",
		strings.Repeat("■", filled), strings.Repeat("·", 10-filled), tokens, limit, ratio*100)
	return lipgloss.NewStyle().Foreground(color).Render(text)
}
