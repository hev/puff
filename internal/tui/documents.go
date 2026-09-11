package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/client"
	"github.com/turbopuffer/turbopuffer-go"
)

type documentsLoadedMsg struct {
	rows []map[string]any
}

type documentsErrMsg struct {
	err error
}

// searchResultsMsg carries FTS results; tagged with the query that produced them
// so late-arriving messages for stale queries can be ignored.
type searchResultsMsg struct {
	query string
	field string
	rows  []map[string]any
}

type searchErrMsg struct {
	query string
	err   error
}

type documentsModel struct {
	namespace    string
	rows         []map[string]any
	cursor   int
	loading  bool
	err      error
	pageSize int
	schemaDict   map[string]any
	contentField string

	// Search state
	searching      bool   // input prompt is active
	searchQuery    string // current input buffer / active query
	searchField    string // FTS field being used
	searchActive   bool   // rows currently reflect FTS results (vs. default list)
	searchErr      error
	searchLoading  bool
	defaultRows    []map[string]any // saved rows to restore when search is cleared
	defaultCursor  int
}

func newDocumentsModel(namespace string) documentsModel {
	return documentsModel{
		namespace: namespace,
		loading:   true,
		pageSize:  50,
	}
}

func (m documentsModel) init(region string, schemaDict map[string]any) tea.Cmd {
	ns := m.namespace
	pageSize := m.pageSize
	return func() tea.Msg {
		return fetchDocuments(ns, region, pageSize, schemaDict)
	}
}

func fetchDocuments(namespace, region string, topK int, schemaDict map[string]any) tea.Msg {
	ctx := context.Background()
	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		return documentsErrMsg{err: err}
	}

	// Get schema to find vector attribute
	if schemaDict == nil {
		md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
		if err != nil {
			return documentsErrMsg{err: err}
		}
		schemaDict = make(map[string]any)
		for k, v := range md.Schema {
			schemaDict[k] = v
		}
	}

	vecAttr, dims := extractVectorInfo(schemaDict)
	if vecAttr == "" {
		return documentsErrMsg{err: fmt.Errorf("no vector attribute found in namespace schema")}
	}

	zeroVec := make([]float32, dims)
	result, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy:            turbopuffer.NewRankByVector(vecAttr, zeroVec),
		TopK:              turbopuffer.Int(int64(topK)),
		ExcludeAttributes: []string{vecAttr},
		Consistency: turbopuffer.NamespaceQueryParamsConsistency{
			Level: turbopuffer.NamespaceQueryParamsConsistencyLevelEventual,
		},
	})
	if err != nil {
		return documentsErrMsg{err: err}
	}

	rows := make([]map[string]any, len(result.Rows))
	for i, r := range result.Rows {
		rows[i] = map[string]any(r)
	}
	return documentsLoadedMsg{rows: rows}
}

func runFTSSearch(namespace, region, field, query string, topK int, schemaDict map[string]any) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		ns, err := client.GetNamespace(namespace, region)
		if err != nil {
			return searchErrMsg{query: query, err: err}
		}

		params := turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(int64(topK)),
			RankBy: turbopuffer.NewRankByTextBM25(field, query),
			Consistency: turbopuffer.NamespaceQueryParamsConsistency{
				Level: turbopuffer.NamespaceQueryParamsConsistencyLevelEventual,
			},
		}
		if vecAttr, _ := extractVectorInfo(schemaDict); vecAttr != "" {
			params.ExcludeAttributes = []string{vecAttr}
		}

		result, err := ns.Query(ctx, params)
		if err != nil {
			return searchErrMsg{query: query, err: err}
		}

		rows := make([]map[string]any, len(result.Rows))
		for i, r := range result.Rows {
			rows[i] = map[string]any(r)
		}
		return searchResultsMsg{query: query, field: field, rows: rows}
	}
}

func (m documentsModel) selected() map[string]any {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

func (m documentsModel) update(msg tea.Msg, region string, schemaDict map[string]any) (documentsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case documentsLoadedMsg:
		m.rows = msg.rows
		m.loading = false
		m.schemaDict = schemaDict
		return m, nil
	case documentsErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	case searchResultsMsg:
		// Ignore stale results from a superseded query
		if msg.query != m.searchQuery {
			return m, nil
		}
		m.rows = msg.rows
		m.cursor = 0
		m.searchActive = true
		m.searchField = msg.field
		m.searchErr = nil
		m.searchLoading = false
		return m, nil
	case searchErrMsg:
		if msg.query != m.searchQuery {
			return m, nil
		}
		m.searchErr = msg.err
		m.searchLoading = false
		return m, nil
	case tea.KeyMsg:
		if m.searching {
			return m.handleSearchInput(msg, region, schemaDict)
		}
		// When a search is active but the prompt is closed, 'esc' clears the
		// search and restores the default listing.
		if m.searchActive && msg.String() == "esc" {
			m.rows = m.defaultRows
			m.cursor = m.defaultCursor
			m.defaultRows = nil
			m.searchActive = false
			m.searchQuery = ""
			m.searchErr = nil
			m.searchLoading = false
			return m, nil
		}
		switch {
		case msg.String() == "j" || msg.String() == "down":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case msg.String() == "k" || msg.String() == "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case msg.String() == "G":
			m.cursor = max(0, len(m.rows)-1)
		case msg.String() == "g":
			m.cursor = 0
		case msg.String() == "/":
			// Enter search mode
			field := resolveFTSField(schemaDict, m.contentField)
			if field == "" {
				m.searchErr = fmt.Errorf("no FTS-enabled field in namespace schema")
				return m, nil
			}
			if !m.searchActive {
				m.defaultRows = m.rows
				m.defaultCursor = m.cursor
			}
			m.searching = true
			m.searchField = field
			m.searchErr = nil
			if !m.searchActive {
				m.searchQuery = ""
			}
		}
	}
	return m, nil
}

func (m documentsModel) handleSearchInput(msg tea.KeyMsg, region string, schemaDict map[string]any) (documentsModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel search: restore previous listing if we had one
		m.searching = false
		if m.searchActive {
			m.rows = m.defaultRows
			m.cursor = m.defaultCursor
			m.defaultRows = nil
			m.searchActive = false
			m.searchQuery = ""
			m.searchErr = nil
			m.searchLoading = false
		} else {
			m.searchQuery = ""
			m.searchErr = nil
		}
		return m, nil
	case "enter":
		q := strings.TrimSpace(m.searchQuery)
		if q == "" {
			m.searching = false
			return m, nil
		}
		m.searching = false
		m.searchLoading = true
		m.searchErr = nil
		m.searchQuery = q
		return m, runFTSSearch(m.namespace, region, m.searchField, q, m.pageSize, schemaDict)
	case "backspace":
		if len(m.searchQuery) > 0 {
			r := []rune(m.searchQuery)
			m.searchQuery = string(r[:len(r)-1])
		}
		return m, nil
	default:
		s := msg.String()
		// Accept any single-rune printable input (handles unicode properly)
		if len([]rune(s)) == 1 {
			m.searchQuery += s
		}
		return m, nil
	}
}

func (m documentsModel) view(width, height int) string {
	if m.loading {
		return loadingStyle.Render(fmt.Sprintf("Loading documents from %s...", m.namespace))
	}
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %s\n\n", m.err)) +
			helpStyle.Render("esc back • q quit")
	}

	var b strings.Builder

	// Header
	title := fmt.Sprintf(" %s (%d docs) ", m.namespace, len(m.rows))
	if m.searchActive {
		title = fmt.Sprintf(" %s — FTS \"%s\" on %s (%d) ", m.namespace, m.searchQuery, m.searchField, len(m.rows))
	}
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	// Column headers
	idW := 24
	if width > 80 {
		idW = 32
	}
	contentsW := width - idW - 8
	if contentsW < 20 {
		contentsW = 40
	}

	contentsLabel := "Contents"
	if m.searchActive && m.searchField != "" {
		contentsLabel = m.searchField
	} else if m.contentField != "" {
		contentsLabel = m.contentField
	}
	header := fmt.Sprintf("  %-*s %s", idW, "ID", contentsLabel)
	b.WriteString(columnHeaderStyle.Render(header))
	b.WriteString("\n")

	// Rows with scrolling
	reservedRows := 6
	if m.searching || m.searchErr != nil || m.searchLoading {
		reservedRows++
	}
	maxRows := height - reservedRows
	if maxRows < 1 {
		maxRows = 1
	}
	offset := 0
	if m.cursor >= maxRows {
		offset = m.cursor - maxRows + 1
	}

	if len(m.rows) == 0 {
		b.WriteString(helpStyle.Render(fmt.Sprintf("  (no %s)", map[bool]string{true: "matches", false: "documents"}[m.searchActive])))
		b.WriteString("\n")
	}

	for i := offset; i < len(m.rows) && i < offset+maxRows; i++ {
		row := m.rows[i]
		id := sanitizeLine(fmt.Sprintf("%v", row["id"]))
		id = truncate(id, idW)

		// In FTS mode, prefer the searched field for the preview.
		previewField := m.contentField
		if m.searchActive && m.searchField != "" {
			previewField = m.searchField
		}
		preview := docPreviewLine(row, m.schemaDict, previewField, contentsW)

		line := fmt.Sprintf("  %-*s %s", idW, id, preview)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("▸ " + line[2:]))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Status / prompt
	b.WriteString("\n")
	switch {
	case m.searching:
		b.WriteString(statusStyle.Render(fmt.Sprintf("FTS (%s): %s█", m.searchField, m.searchQuery)))
		b.WriteString("\n")
	case m.searchLoading:
		b.WriteString(loadingStyle.Render(fmt.Sprintf("Searching for \"%s\"...", m.searchQuery)))
		b.WriteString("\n")
	case m.searchErr != nil:
		b.WriteString(errorStyle.Render(fmt.Sprintf("Search error: %s", m.searchErr)))
		b.WriteString("\n")
	}

	// Help
	if m.searching {
		b.WriteString(helpStyle.Render("type query • enter run • esc cancel"))
	} else if m.searchActive {
		b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter preview • / new search • esc clear • s schema • q back"))
	} else {
		b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter preview • / FTS search • s schema • esc back"))
	}

	return b.String()
}
