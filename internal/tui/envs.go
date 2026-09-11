package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/config"
)

type envsModel struct {
	entries []config.EnvEntry
	cursor  int
}

func newEnvsModel() envsModel {
	entries := config.ListEnvs()
	sort.Slice(entries, func(i, j int) bool {
		// active first, then alphabetical
		if entries[i].IsActive != entries[j].IsActive {
			return entries[i].IsActive
		}
		return entries[i].Name < entries[j].Name
	})
	cursor := 0
	for i, e := range entries {
		if e.IsActive {
			cursor = i
			break
		}
	}
	return envsModel{entries: entries, cursor: cursor}
}

func (m envsModel) selected() *config.EnvEntry {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}

func (m envsModel) update(msg tea.Msg) (envsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.entries) > 0 {
				m.cursor = len(m.entries) - 1
			}
		}
	}
	return m, nil
}

func (m envsModel) view(width, height int) string {
	var b strings.Builder

	title := fmt.Sprintf(" Environments (%d) ", len(m.entries))
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if len(m.entries) == 0 {
		b.WriteString("No environments configured. Run 'puff env add <name>' to add one.\n\n")
		b.WriteString(helpStyle.Render("q quit"))
		return b.String()
	}

	// Column widths
	nameW := 20
	regionW := 20
	keyW := 14
	for _, e := range m.entries {
		if len(e.Name) > nameW {
			nameW = len(e.Name)
		}
		if len(e.Config.Region) > regionW {
			regionW = len(e.Config.Region)
		}
	}
	if nameW > 40 {
		nameW = 40
	}

	header := fmt.Sprintf("    %-*s %-*s %-*s", nameW, "Name", regionW, "Region", keyW, "API Key")
	b.WriteString(columnHeaderStyle.Render(header))
	b.WriteString("\n")

	maxRows := height - 6
	if maxRows < 1 {
		maxRows = len(m.entries)
	}
	offset := 0
	if m.cursor >= maxRows {
		offset = m.cursor - maxRows + 1
	}

	for i := offset; i < len(m.entries) && i < offset+maxRows; i++ {
		e := m.entries[i]
		marker := " "
		if e.IsActive {
			marker = "*"
		}
		name := truncate(sanitizeLine(e.Name), nameW)
		region := truncate(sanitizeLine(e.Config.Region), regionW)
		key := truncate(config.MaskKey(e.Config.APIKey), keyW)
		line := fmt.Sprintf("  %s %-*s %-*s %-*s", marker, nameW, name, regionW, region, keyW, key)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("▸ " + line[2:]))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter select • q quit"))
	return b.String()
}
