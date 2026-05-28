package ui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#5f00af"))

	leftPaneStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	rightPaneStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#5f00af"))

	itemStyle = lipgloss.NewStyle()

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(0, 1)

	stateStyles = map[string]lipgloss.Style{
		"waiting":         lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		"checking remote": lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		"building image":  lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		"pushing image":   lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		"done":            lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"already pushed":  lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"failed":          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		"cancelled":       lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	}
)

func stateStyle(state string) lipgloss.Style {
	if s, ok := stateStyles[state]; ok {
		return s
	}
	return itemStyle
}
