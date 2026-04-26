package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("87"))
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	priorityStyles = map[Priority]lipgloss.Style{
		PriorityNone:             lipgloss.NewStyle(),
		PriorityDirty:            lipgloss.NewStyle().Foreground(lipgloss.Color("87")),
		PriorityReviewWaiting:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		PriorityChangesRequested: lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		PriorityCIFail:           lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}
)

func stylePriority(p Priority, s string) string {
	style, ok := priorityStyles[p]
	if !ok {
		return s
	}
	return style.Render(s)
}
