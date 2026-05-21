package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("39")  // blue
	colorSecondary = lipgloss.Color("245") // gray
	colorWarning   = lipgloss.Color("214") // orange
	colorMuted     = lipgloss.Color("240") // dark gray

	// Header bar
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(colorPrimary).
			Padding(0, 1)

	// Status bar at bottom
	statusStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	// Help text
	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Selected row
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	// Normal row
	normalStyle = lipgloss.NewStyle()

	// Column header
	columnHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSecondary).
				Underline(true)

	// Error style
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	// Loading style
	loadingStyle = lipgloss.NewStyle().
			Foreground(colorWarning)
)
