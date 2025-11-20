package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/marcuzy/logsviewer/internal/logs"
)

const statusBarHeight = 1

// Model implements the Bubble Tea program for the logs viewer.
type Model struct {
	list     list.Model
	viewport viewport.Model

	entries        []logs.LogEntry
	displayEntries []logs.LogEntry

	entryCh <-chan logs.LogEntry
	errCh   <-chan error
	cancel  context.CancelFunc

	extraFields     []string
	extraFieldIndex int
	maxEntries      int

	width  int
	height int
	ready  bool

	statusMessage string
	errorMessage  string

	searchActive     bool
	searchInput      textinput.Model
	searchQuery      string
	searchMatchCount int

	focus            focusArea
	needViewportSync bool

	styles styles
}

type focusArea int

const (
	focusList focusArea = iota
	focusDetail
)

func (f focusArea) String() string {
	switch f {
	case focusDetail:
		return "detail"
	default:
		return "list"
	}
}

// Options configures the UI model.
type Options struct {
	Entries  <-chan logs.LogEntry
	Errors   <-chan error
	Cancel   context.CancelFunc
	Extra    []string
	MaxItems int
}

// NewModel constructs a Model with sensible defaults.
func NewModel(opts Options) Model {
	st := defaultStyles()

	items := []list.Item{}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	ls := list.New(items, delegate, 0, 0)
	ls.Title = "Logs"
	ls.SetShowHelp(false)
	ls.SetShowStatusBar(false)
	ls.SetFilteringEnabled(true)

	vp := viewport.New(0, 0)
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "search"
	ti.CharLimit = 256
	ti.Blur()

	return Model{
		list:          ls,
		viewport:      vp,
		entryCh:       opts.Entries,
		errCh:         opts.Errors,
		cancel:        opts.Cancel,
		extraFields:   append([]string(nil), opts.Extra...),
		maxEntries:    opts.MaxItems,
		statusMessage: "tailing...",
		searchInput:   ti,
		focus:         focusList,
		styles:        st,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.waitForEntry(), m.waitForError())
}

// Update reacts to incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	prevSelection := m.selectionKey()
	var sendKeyToList bool
	var sendKeyToViewport bool
	var keyHandled bool

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.searchActive {
			switch key {
			case "enter":
				m.commitSearch(strings.TrimSpace(m.searchInput.Value()))
			case "esc":
				m.cancelSearchInput()
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			keyHandled = true
			break
		}
		switch key {
		case "q":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "/":
			m.beginSearch()
			keyHandled = true
		case "esc":
			if m.searchQuery != "" {
				m.applySearch("")
				m.statusMessage = "search cleared"
				keyHandled = true
			}
		case "f":
			if len(m.extraFields) > 1 {
				m.extraFieldIndex = (m.extraFieldIndex + 1) % len(m.extraFields)
				m.rebuildList()
				m.statusMessage = fmt.Sprintf("extra field: %s", m.currentExtraField())
				keyHandled = true
			}
		case "n":
			if m.searchQuery != "" {
				m.stepMatch(1)
				if m.searchMatchCount > 0 {
					m.statusMessage = fmt.Sprintf("match %d/%d", m.list.Index()+1, m.searchMatchCount)
				}
				keyHandled = true
			}
		case "N":
			if m.searchQuery != "" {
				m.stepMatch(-1)
				if m.searchMatchCount > 0 {
					m.statusMessage = fmt.Sprintf("match %d/%d", m.list.Index()+1, m.searchMatchCount)
				}
				keyHandled = true
			}
		case "tab", "right":
			if m.focus != focusDetail {
				m.focus = focusDetail
				keyHandled = true
			}
		case "shift+tab", "left", "backtab":
			if m.focus != focusList {
				m.focus = focusList
				keyHandled = true
			}
		}
		if !keyHandled {
			if m.focus == focusDetail {
				sendKeyToViewport = true
			} else {
				sendKeyToList = true
			}
		}
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
	case logEntryMsg:
		m.appendEntry(msg.entry)
		cmds = append(cmds, m.waitForEntry())
	case streamClosedMsg:
		m.entryCh = nil
		m.statusMessage = "input stream closed"
	case errMsg:
		m.errorMessage = msg.err.Error()
		cmds = append(cmds, m.waitForError())
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if sendKeyToList {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if sendKeyToViewport {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.viewport, cmd = m.viewport.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	newSelection := m.selectionKey()
	if m.needViewportSync || newSelection != prevSelection {
		m.updateViewportFromSelection()
		m.needViewportSync = false
	}

	return m, tea.Batch(cmds...)
}

// View renders the UI.
func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}

	listView := m.styles.list.Render(m.list.View())
	detailView := m.styles.detail.Render(m.viewport.View())
	content := lipgloss.JoinHorizontal(lipgloss.Top, listView, detailView)

	status := m.statusLine()
	if status != "" {
		footer := m.styles.status.Render(status)
		return lipgloss.JoinVertical(lipgloss.Left, content, footer)
	}
	return content
}

func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.ready = true

	listWidth := m.width / 2
	if listWidth < 40 {
		listWidth = 40
	}
	if listWidth > m.width-20 {
		listWidth = m.width - 20
	}

	listHeight := m.height - statusBarHeight
	if listHeight < 3 {
		listHeight = 3
	}

	detailWidth := m.width - listWidth
	if detailWidth < 20 {
		detailWidth = 20
	}
	detailHeight := listHeight

	m.list.SetSize(listWidth, listHeight)
	m.viewport.Width = detailWidth
	m.viewport.Height = detailHeight
	m.viewport.SetContent(m.viewport.View())
	if m.width > 8 {
		m.searchInput.Width = m.width - 8
	} else {
		m.searchInput.Width = m.width
	}
}

func (m *Model) appendEntry(entry logs.LogEntry) {
	m.entries = append([]logs.LogEntry{entry}, m.entries...)
	if m.maxEntries > 0 && len(m.entries) > m.maxEntries {
		m.entries = m.entries[:m.maxEntries]
	}

	m.rebuildList()

	if m.list.Index() <= 0 {
		m.list.Select(0)
	}
}

func (m *Model) rebuildList() {
	entries := m.filteredEntries()
	m.displayEntries = entries
	m.searchMatchCount = len(entries)
	m.needViewportSync = true

	items := make([]list.Item, len(entries))
	extraField := m.currentExtraField()
	highlightQuery := strings.TrimSpace(m.searchQuery)
	for i, entry := range entries {
		items[i] = logItem{
			entry:          entry,
			extraField:     extraField,
			highlightQuery: highlightQuery,
			highlightStyle: m.styles.highlight,
		}
	}

	curIndex := m.list.Index()
	if curIndex < 0 {
		curIndex = 0
	}

	m.list.SetItems(items)

	if len(items) == 0 {
		m.list.ResetSelected()
		m.needViewportSync = true
		return
	}

	if curIndex >= len(items) {
		curIndex = len(items) - 1
	}
	m.list.Select(curIndex)
}

func (m *Model) updateViewportFromSelection() {
	defer func() { m.needViewportSync = false }()
	item := m.list.SelectedItem()
	if item == nil {
		m.viewport.SetContent("")
		return
	}
	logItem, ok := item.(logItem)
	if !ok {
		return
	}
	content := logItem.entry.PrettyJSON()
	if content == "" {
		content = logItem.entry.Raw
	}
	content = highlightText(content, m.searchQuery, m.styles.highlight)
	m.viewport.SetContent(content)
}

func (m *Model) filteredEntries() []logs.LogEntry {
	if m.searchQuery == "" {
		return append([]logs.LogEntry(nil), m.entries...)
	}
	query := strings.ToLower(m.searchQuery)
	matches := make([]logs.LogEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		if entryMatchesQuery(entry, query) {
			matches = append(matches, entry)
		}
	}
	return matches
}

func (m Model) statusLine() string {
	var parts []string
	if m.searchActive {
		parts = append(parts, "search "+m.searchInput.View())
	} else if m.searchQuery != "" {
		parts = append(parts, fmt.Sprintf("/%s (%d)", m.searchQuery, m.searchMatchCount))
	}
	parts = append(parts, fmt.Sprintf("focus: %s", m.focus.String()))
	if extra := m.currentExtraField(); extra != "" {
		parts = append(parts, fmt.Sprintf("extra: %s", extra))
	}
	if m.statusMessage != "" {
		parts = append(parts, m.statusMessage)
	}
	if m.errorMessage != "" {
		parts = append(parts, "error: "+m.errorMessage)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  |  ")
}

func (m Model) currentExtraField() string {
	if len(m.extraFields) == 0 {
		return ""
	}
	return m.extraFields[m.extraFieldIndex%len(m.extraFields)]
}

func (m Model) waitForEntry() tea.Cmd {
	if m.entryCh == nil {
		return nil
	}
	return func() tea.Msg {
		entry, ok := <-m.entryCh
		if !ok {
			return streamClosedMsg{}
		}
		return logEntryMsg{entry: entry}
	}
}

func (m Model) waitForError() tea.Cmd {
	if m.errCh == nil {
		return nil
	}
	return func() tea.Msg {
		err, ok := <-m.errCh
		if !ok {
			return nil
		}
		return errMsg{err: err}
	}
}

type logEntryMsg struct {
	entry logs.LogEntry
}

type streamClosedMsg struct{}

type errMsg struct {
	err error
}

func highlightText(text, query string, style lipgloss.Style) string {
	query = strings.TrimSpace(query)
	if text == "" || query == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)

	var b strings.Builder
	start := 0
	for {
		idx := strings.Index(lowerText[start:], lowerQuery)
		if idx == -1 {
			b.WriteString(text[start:])
			break
		}
		idx += start
		end := idx + len(query)
		b.WriteString(text[start:idx])
		b.WriteString(style.Render(text[idx:end]))
		start = end
	}

	return b.String()
}

type logItem struct {
	entry          logs.LogEntry
	extraField     string
	highlightQuery string
	highlightStyle lipgloss.Style
}

func (i logItem) Title() string {
	ts := i.entry.DisplayTimestamp()
	message := i.entry.Message
	if message == "" {
		message = i.entry.Raw
	}
	if ts != "" {
		return highlightText(fmt.Sprintf("%s  %s", ts, message), i.highlightQuery, i.highlightStyle)
	}
	return highlightText(message, i.highlightQuery, i.highlightStyle)
}

func (i logItem) Description() string {
	val := i.entry.ExtraValue(i.extraField)
	if val == "" {
		return highlightText(i.entry.Path, i.highlightQuery, i.highlightStyle)
	}
	return highlightText(val, i.highlightQuery, i.highlightStyle)
}

func (i logItem) FilterValue() string {
	values := []string{i.entry.Message, i.entry.Raw, i.entry.DisplayTimestamp()}
	for k, v := range i.entry.Extras {
		values = append(values, fmt.Sprintf("%s:%s", k, v))
	}
	return strings.Join(values, " ")
}

type styles struct {
	list      lipgloss.Style
	detail    lipgloss.Style
	status    lipgloss.Style
	highlight lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		list:      lipgloss.NewStyle().Padding(0, 1, 0, 1),
		detail:    lipgloss.NewStyle().Padding(0, 1, 0, 1),
		status:    lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("244")),
		highlight: lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("229")),
	}
}

func (m *Model) beginSearch() {
	m.searchActive = true
	m.focus = focusList
	m.searchInput.SetValue(m.searchQuery)
	m.searchInput.CursorEnd()
	m.searchInput.Focus()
}

func (m *Model) cancelSearchInput() {
	m.searchActive = false
	m.searchInput.Blur()
	m.searchInput.SetValue("")
}

func (m *Model) commitSearch(query string) {
	m.searchActive = false
	m.searchInput.Blur()
	m.searchInput.SetValue("")
	m.applySearch(query)
	if query != "" {
		m.statusMessage = fmt.Sprintf("search %q", query)
	} else {
		m.statusMessage = "search cleared"
	}
}

func (m *Model) applySearch(query string) {
	m.searchQuery = query
	m.rebuildList()
	if len(m.displayEntries) == 0 {
		m.list.ResetSelected()
		m.viewport.SetContent("")
		m.updateViewportFromSelection()
		return
	}
	idx := m.list.Index()
	if idx < 0 || idx >= len(m.displayEntries) {
		m.list.Select(0)
	}
	m.updateViewportFromSelection()
}

func (m *Model) stepMatch(delta int) {
	count := len(m.displayEntries)
	if count == 0 {
		return
	}
	idx := m.list.Index()
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % count
	if idx < 0 {
		idx += count
	}
	m.list.Select(idx)
	m.needViewportSync = true
}

func (m Model) selectionKey() string {
	if item, ok := m.list.SelectedItem().(logItem); ok {
		return item.entry.Path + "\x00" + item.entry.Raw
	}
	return ""
}

func entryMatchesQuery(entry logs.LogEntry, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Message), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Raw), query) {
		return true
	}
	if ts := entry.DisplayTimestamp(); ts != "" && strings.Contains(strings.ToLower(ts), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Path), query) {
		return true
	}
	for _, val := range entry.Extras {
		if strings.Contains(strings.ToLower(val), query) {
			return true
		}
	}
	return false
}
