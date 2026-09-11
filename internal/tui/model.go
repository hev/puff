package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/config"
)

type view int

const (
	viewEnvs view = iota
	viewNamespaces
	viewDocuments
	viewPreview
	viewDocument
	viewSchema
)

// Model is the top-level Bubble Tea model.
type Model struct {
	view   view
	region string
	width  int
	height int

	// Sub-models
	envs       envsModel
	namespaces namespacesModel
	documents  documentsModel
	preview    previewModel
	document   documentModel
	schema     schemaModel

	// Navigation context
	selectedNamespace string
	schemaDict        map[string]any // cached schema for selected namespace
	prevView          view           // for schema back-nav
}

// New creates a new TUI model.
func New(region string) Model {
	// Start on env-switcher only if >1 env is configured AND no region override is set
	// (a region override implies the caller knows which env/region they want).
	envs := config.ListEnvs()
	if len(envs) > 1 && region == "" {
		return Model{
			view: viewEnvs,
			envs: newEnvsModel(),
		}
	}
	return Model{
		view:       viewNamespaces,
		region:     region,
		namespaces: newNamespacesModel(),
	}
}

func (m Model) Init() tea.Cmd {
	if m.view == viewEnvs {
		return nil
	}
	return m.namespaces.init(m.region)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		// Global quit from any view
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.view {
	case viewEnvs:
		return m.updateEnvs(msg)
	case viewNamespaces:
		return m.updateNamespaces(msg)
	case viewDocuments:
		return m.updateDocuments(msg)
	case viewPreview:
		return m.updatePreview(msg)
	case viewDocument:
		return m.updateDocument(msg)
	case viewSchema:
		return m.updateSchema(msg)
	}
	return m, nil
}

func (m Model) View() string {
	switch m.view {
	case viewEnvs:
		return m.envs.view(m.width, m.height)
	case viewNamespaces:
		return m.namespaces.view(m.width, m.height)
	case viewDocuments:
		return m.documents.view(m.width, m.height)
	case viewPreview:
		return m.preview.view(m.width, m.height)
	case viewDocument:
		return m.document.view(m.width, m.height)
	case viewSchema:
		return m.schema.view(m.width, m.height)
	}
	return ""
}

func (m Model) updateEnvs(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "enter":
			if e := m.envs.selected(); e != nil {
				if err := config.SetActive(e.Name); err != nil {
					// Stay in env view on error; leader will show next refresh
					return m, nil
				}
				client.ClearCache()
				m.region = "" // selected env takes precedence over any prior override
				m.namespaces = newNamespacesModel()
				m.view = viewNamespaces
				return m, m.namespaces.init(m.region)
			}
		}
	}
	var cmd tea.Cmd
	m.envs, cmd = m.envs.update(msg)
	return m, cmd
}

func (m Model) updateNamespaces(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case namespacesTickMsg:
		if !m.namespaces.loading {
			return m, m.namespaces.refresh(m.region)
		}
		return m, namespacesTickCmd()
	case tea.KeyMsg:
		switch {
		case msg.String() == "q" && !m.namespaces.filtering:
			return m, tea.Quit
		case msg.String() == "enter" && !m.namespaces.filtering:
			if ns := m.namespaces.selected(); ns != nil {
				m.selectedNamespace = ns.NamespaceID
				m.schemaDict = nil
				if ns.Metadata != nil {
					m.schemaDict = ns.Metadata.Schema
				}
				m.documents = newDocumentsModel(m.selectedNamespace)
				m.documents.contentField = config.GetActiveContentField(m.selectedNamespace)
				m.view = viewDocuments
				return m, m.documents.init(m.region, m.schemaDict)
			}
		case msg.String() == "s" && !m.namespaces.filtering:
			if ns := m.namespaces.selected(); ns != nil {
				m.selectedNamespace = ns.NamespaceID
				m.prevView = viewNamespaces
				m.schema = newSchemaModel(m.selectedNamespace)
				m.view = viewSchema
				return m, m.schema.init(m.region)
			}
		case msg.String() == "e" && !m.namespaces.filtering:
			if len(config.ListEnvs()) > 1 {
				m.envs = newEnvsModel()
				m.view = viewEnvs
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.namespaces, cmd = m.namespaces.update(msg)
	return m, cmd
}

func (m Model) updateDocuments(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// While the search input is open, let the docs model consume keys
		// (esc/enter there control the prompt, not navigation).
		if m.documents.searching {
			break
		}
		switch {
		case msg.String() == "esc":
			// If a search is active, clear it instead of navigating back.
			if m.documents.searchActive {
				break
			}
			m.view = viewNamespaces
			return m, nil
		case msg.String() == "q":
			m.view = viewNamespaces
			return m, nil
		case msg.String() == "enter":
			if doc := m.documents.selected(); doc != nil {
				content := formatDocJSON(doc, m.schemaDict)
				id := fmt.Sprintf("%v", doc["id"])
				m.preview = newPreviewModel(id, content)
				m.preview.setSize(m.width, m.height)
				m.view = viewPreview
				return m, nil
			}
		case msg.String() == "s":
			m.prevView = viewDocuments
			m.schema = newSchemaModel(m.selectedNamespace)
			m.view = viewSchema
			return m, m.schema.init(m.region)
		}
	}
	var cmd tea.Cmd
	m.documents, cmd = m.documents.update(msg, m.region, m.schemaDict)
	return m, cmd
}

func (m Model) updatePreview(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "q":
			m.view = viewDocuments
			return m, nil
		case msg.String() == "enter":
			m.document = newDocumentModel(m.preview.docID, m.preview.content)
			m.document.setSize(m.width, m.height)
			m.view = viewDocument
			return m, nil
		}
	}
	m.preview = m.preview.update(msg)
	return m, nil
}

func (m Model) updateDocument(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "q":
			m.view = viewPreview
			return m, nil
		}
	}
	m.document = m.document.update(msg)
	return m, nil
}

func (m Model) updateSchema(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "q":
			m.view = m.prevView
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.schema, cmd = m.schema.update(msg)
	return m, cmd
}
