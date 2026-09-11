package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/metadata"
)

const namespacesRefreshInterval = 30 * time.Second

// Messages
type namespacesLoadedMsg struct {
	items []metadata.NamespaceWithMetadata
}

type namespacesRefreshedMsg struct {
	items []metadata.NamespaceWithMetadata
}

type namespacesErrMsg struct {
	err error
}

type namespacesTickMsg struct{}

type namespacesModel struct {
	items     []metadata.NamespaceWithMetadata
	filtered  []int // indices into items
	cursor    int
	loading   bool
	err       error
	filter    string
	filtering bool
}

func newNamespacesModel() namespacesModel {
	return namespacesModel{loading: true}
}

func (m namespacesModel) init(region string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		items := metadata.FetchNamespacesWithMetadata(ctx, false, region, false)
		if items == nil {
			return namespacesErrMsg{err: fmt.Errorf("failed to fetch namespaces")}
		}
		// Sort by updated_at descending
		sort.Slice(items, func(i, j int) bool {
			ti := time.Time{}
			tj := time.Time{}
			if items[i].Metadata != nil {
				ti = items[i].Metadata.UpdatedAt
			}
			if items[j].Metadata != nil {
				tj = items[j].Metadata.UpdatedAt
			}
			return ti.After(tj)
		})
		return namespacesLoadedMsg{items: items}
	}
}

func namespacesTickCmd() tea.Cmd {
	return tea.Tick(namespacesRefreshInterval, func(time.Time) tea.Msg {
		return namespacesTickMsg{}
	})
}

func (m namespacesModel) refresh(region string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		items := metadata.FetchNamespacesWithMetadata(ctx, false, region, false)
		if items == nil {
			return nil // silently ignore refresh errors
		}
		sort.Slice(items, func(i, j int) bool {
			ti := time.Time{}
			tj := time.Time{}
			if items[i].Metadata != nil {
				ti = items[i].Metadata.UpdatedAt
			}
			if items[j].Metadata != nil {
				tj = items[j].Metadata.UpdatedAt
			}
			return ti.After(tj)
		})
		return namespacesRefreshedMsg{items: items}
	}
}

func (m namespacesModel) selected() *metadata.NamespaceWithMetadata {
	indices := m.visibleIndices()
	if m.cursor < 0 || m.cursor >= len(indices) {
		return nil
	}
	return &m.items[indices[m.cursor]]
}

func (m namespacesModel) visibleIndices() []int {
	if len(m.filtered) > 0 || m.filter != "" {
		return m.filtered
	}
	indices := make([]int, len(m.items))
	for i := range m.items {
		indices[i] = i
	}
	return indices
}

func (m *namespacesModel) applyFilter() {
	if m.filter == "" {
		m.filtered = nil
		return
	}
	m.filtered = nil
	lower := strings.ToLower(m.filter)
	for i, item := range m.items {
		if strings.Contains(strings.ToLower(item.NamespaceID), lower) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.visibleIndices()) {
		m.cursor = max(0, len(m.visibleIndices())-1)
	}
}

func (m namespacesModel) update(msg tea.Msg) (namespacesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case namespacesLoadedMsg:
		m.items = msg.items
		m.loading = false
		m.applyFilter()
		return m, namespacesTickCmd()
	case namespacesRefreshedMsg:
		m.items = msg.items
		m.applyFilter()
		visible := m.visibleIndices()
		if m.cursor >= len(visible) {
			m.cursor = max(0, len(visible)-1)
		}
		return m, namespacesTickCmd()
	case namespacesErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filter = ""
				m.applyFilter()
			case "enter":
				m.filtering = false
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.applyFilter()
				}
			default:
				if len(msg.String()) == 1 {
					m.filter += msg.String()
					m.applyFilter()
				}
			}
			return m, nil
		}

		visible := m.visibleIndices()
		switch {
		case msg.String() == "j" || msg.String() == "down":
			if m.cursor < len(visible)-1 {
				m.cursor++
			}
		case msg.String() == "k" || msg.String() == "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case msg.String() == "G":
			m.cursor = max(0, len(visible)-1)
		case msg.String() == "g":
			m.cursor = 0
		case msg.String() == "/":
			m.filtering = true
			m.filter = ""
		}
	}
	return m, nil
}

func (m namespacesModel) view(width, height int) string {
	if m.loading {
		return loadingStyle.Render("Loading namespaces...")
	}
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %s", m.err))
	}
	if len(m.items) == 0 {
		return "No namespaces found"
	}

	var b strings.Builder

	// Header
	title := fmt.Sprintf(" Namespaces (%d) ", len(m.items))
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	// Column headers
	nameW := 30
	rowsW := 10
	sizeW := 12
	statusW := 14
	unindexedW := 12
	updatedW := 16

	// Adapt name width to terminal
	if width > 100 {
		nameW = width - rowsW - sizeW - statusW - unindexedW - updatedW - 18
	}

	header := fmt.Sprintf("  %-*s %*s %*s %-*s %*s %*s",
		nameW, "Name", rowsW, "Rows", sizeW, "Size", statusW, "Index Status", unindexedW, "Unindexed", updatedW, "Updated")
	b.WriteString(columnHeaderStyle.Render(header))
	b.WriteString("\n")

	// Rows
	visible := m.visibleIndices()
	maxRows := height - 6 // header + status + help
	if m.filtering {
		maxRows--
	}

	// Scrolling offset
	offset := 0
	if m.cursor >= maxRows {
		offset = m.cursor - maxRows + 1
	}

	for i := offset; i < len(visible) && i < offset+maxRows; i++ {
		item := m.items[visible[i]]
		name := truncate(item.NamespaceID, nameW)
		rows := "N/A"
		size := "N/A"
		status := "N/A"
		updated := "N/A"

		unindexed := ""

		if item.Metadata != nil {
			rows = formatNumber(item.Metadata.ApproxRowCount)
			size = formatBytes(item.Metadata.ApproxLogicalBytes)
			status = metadata.GetIndexStatus(item.Metadata)
			updated = formatUpdatedAt(item.Metadata.UpdatedAt)
			if ub := metadata.GetUnindexedBytes(item.Metadata); ub > 0 {
				unindexed = formatBytes(ub)
			}
		}

		line := fmt.Sprintf("  %-*s %*s %*s %-*s %*s %*s",
			nameW, name, rowsW, rows, sizeW, size, statusW, status, unindexedW, unindexed, updatedW, updated)

		if i == m.cursor {
			b.WriteString(selectedStyle.Render("▸ " + line[2:]))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Filter bar
	if m.filtering {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(fmt.Sprintf("Filter: %s█", m.filter)))
		b.WriteString("\n")
	}

	// Status & help
	b.WriteString("\n")
	if m.filter != "" && !m.filtering {
		b.WriteString(statusStyle.Render(fmt.Sprintf("Filtered: %d/%d", len(visible), len(m.items))))
		b.WriteString("  ")
	}
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter select • s schema • / filter • e env • q quit"))

	return b.String()
}
