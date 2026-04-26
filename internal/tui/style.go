package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("87"))

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))

	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))

	statusStyles = map[string]lipgloss.Style{
		"draft":     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		"active":    lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		"blocked":   lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		"merged":    lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		"abandoned": lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}

	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

func styleStatus(status, padded string) string {
	if st, ok := statusStyles[status]; ok {
		return st.Render(padded)
	}
	return padded
}
