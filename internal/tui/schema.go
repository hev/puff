package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/client"
	"github.com/turbopuffer/turbopuffer-go"
)

type schemaLoadedMsg struct {
	schema map[string]any
}

type schemaErrMsg struct {
	err error
}

type schemaModel struct {
	namespace string
	schema    map[string]any
	loading   bool
	err       error
	cursor    int
	attrs     []schemaAttr // sorted attributes
}

type schemaAttr struct {
	name       string
	typeStr    string
	filterable string
	fts        string
}

func newSchemaModel(namespace string) schemaModel {
	return schemaModel{
		namespace: namespace,
		loading:   true,
	}
}

func (m schemaModel) init(region string) tea.Cmd {
	ns := m.namespace
	return func() tea.Msg {
		ctx := context.Background()
		nsRef, err := client.GetNamespace(ns, region)
		if err != nil {
			return schemaErrMsg{err: err}
		}

		md, err := nsRef.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
		if err != nil {
			return schemaErrMsg{err: err}
		}

		schemaDict := make(map[string]any)
		for k, v := range md.Schema {
			schemaDict[k] = v
		}
		return schemaLoadedMsg{schema: schemaDict}
	}
}

func parseSchemaAttrs(schema map[string]any) []schemaAttr {
	var attrs []schemaAttr
	for name, val := range schema {
		a := schemaAttr{name: name}

		switch v := val.(type) {
		case string:
			a.typeStr = v
			a.filterable = "-"
			a.fts = "-"
		case map[string]any:
			if t, ok := v["type"].(string); ok {
				a.typeStr = t
			}
			if f, ok := v["filterable"].(bool); ok && f {
				a.filterable = "yes"
			} else {
				a.filterable = "-"
			}
			if ftsVal, ok := v["full_text_search"]; ok && ftsEnabled(ftsVal) {
				a.fts = "yes"
			} else {
				a.fts = "-"
			}
		default:
			b, _ := json.Marshal(val)
			var obj struct {
				Type           string `json:"type"`
				Filterable     *bool  `json:"filterable"`
				FullTextSearch any    `json:"full_text_search"`
			}
			if json.Unmarshal(b, &obj) == nil {
				a.typeStr = obj.Type
				if obj.Filterable != nil && *obj.Filterable {
					a.filterable = "yes"
				} else {
					a.filterable = "-"
				}
				if obj.FullTextSearch != nil && ftsEnabled(obj.FullTextSearch) {
					a.fts = "yes"
				} else {
					a.fts = "-"
				}
			} else {
				a.typeStr = fmt.Sprintf("%v", val)
				a.filterable = "-"
				a.fts = "-"
			}
		}

		attrs = append(attrs, a)
	}

	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].name < attrs[j].name
	})
	return attrs
}

func (m schemaModel) update(msg tea.Msg) (schemaModel, tea.Cmd) {
	switch msg := msg.(type) {
	case schemaLoadedMsg:
		m.schema = msg.schema
		m.attrs = parseSchemaAttrs(msg.schema)
		m.loading = false
		return m, nil
	case schemaErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	case tea.KeyMsg:
		switch {
		case msg.String() == "j" || msg.String() == "down":
			if m.cursor < len(m.attrs)-1 {
				m.cursor++
			}
		case msg.String() == "k" || msg.String() == "up":
			if m.cursor > 0 {
				m.cursor--
			}
		}
	}
	return m, nil
}

func (m schemaModel) view(width, height int) string {
	if m.loading {
		return loadingStyle.Render(fmt.Sprintf("Loading schema for %s...", m.namespace))
	}
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %s\n\n", m.err)) +
			helpStyle.Render("esc back")
	}
	if len(m.attrs) == 0 {
		return fmt.Sprintf("No schema found for namespace: %s\n\n", m.namespace) +
			helpStyle.Render("esc back")
	}

	var b strings.Builder

	title := fmt.Sprintf(" Schema: %s (%d attributes) ", m.namespace, len(m.attrs))
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	// Calculate column widths
	nameW := 20
	typeW := 16
	filterW := 10
	ftsW := 6

	for _, a := range m.attrs {
		if len(a.name) > nameW {
			nameW = len(a.name)
		}
		if len(a.typeStr) > typeW {
			typeW = len(a.typeStr)
		}
	}
	if nameW > 40 {
		nameW = 40
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s",
		nameW, "Attribute", typeW, "Type", filterW, "Filterable", ftsW, "FTS")
	b.WriteString(columnHeaderStyle.Render(header))
	b.WriteString("\n")

	maxRows := height - 6
	offset := 0
	if m.cursor >= maxRows {
		offset = m.cursor - maxRows + 1
	}

	for i := offset; i < len(m.attrs) && i < offset+maxRows; i++ {
		a := m.attrs[i]
		name := truncate(a.name, nameW)
		line := fmt.Sprintf("  %-*s %-*s %-*s %-*s",
			nameW, name, typeW, a.typeStr, filterW, a.filterable, ftsW, a.fts)

		if i == m.cursor {
			b.WriteString(selectedStyle.Render("▸ " + line[2:]))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • esc back"))

	return b.String()
}
