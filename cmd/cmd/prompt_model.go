package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

type promptCompleteFunc func(context.Context, string, int) completionResult

type promptSubmitFunc func(context.Context, string) tea.Cmd

type promptModelOptions struct {
	Context       context.Context
	Lifetime      context.Context
	Username      string
	Version       string
	Connected     bool
	Complete      promptCompleteFunc
	Submit        promptSubmitFunc
	Events        <-chan renderer.Event
	History       []string
	HistoryLimit  int
	Startup       tea.Cmd
	StartupCancel context.CancelFunc
	AuthRequests  <-chan *tuiAuthCodeRequest
}

type promptState uint8

const (
	promptStateStarting promptState = iota
	promptStateAuth
	promptStateReady
	promptStateFailed
	promptStateStopping
)

type promptModel struct {
	width, height     int
	username, version string
	connected         bool
	ctx               context.Context
	lifetime          context.Context
	editor            textinput.Model
	viewport          viewport.Model
	progressFrame     int
	progressTicking   bool
	complete          promptCompleteFunc
	submit            promptSubmitFunc
	events            <-chan renderer.Event
	completions       []promptCandidate
	selected          int
	completionOffset  int
	running           bool
	quitting          bool
	cancel            context.CancelFunc
	activeCommandDone <-chan struct{}
	history           []string
	historyIndex      int
	historyLimit      int
	pendingDone       map[string]promptCommandDoneMsg
	barriers          map[string]struct{}
	completedRuns     map[string]struct{}

	completionStart, completionEnd int
	completionQuoted               bool
	completionQuoteClosed          bool
	completionStatus               string
	transcript                     []string
	outputBlocks                   []promptOutputBlock
	activeRows                     map[string]renderer.Event
	activeRowOrder                 []string
	activeRowObservedAt            map[string]time.Time
	terminalRows                   map[string]struct{}
	followBottom                   bool
	state                          promptState
	startup                        tea.Cmd
	startupCancel                  context.CancelFunc
	startupErr                     error
	authRequests                   <-chan *tuiAuthCodeRequest
	authRequest                    *tuiAuthCodeRequest
}

type promptCommandDoneMsg struct {
	RunID         string
	Line          string
	Args          []string
	Err           error
	HistoryStored bool
	HistoryOK     bool
}

type promptRendererEventMsg struct {
	event renderer.Event
}

type promptRendererEventsClosedMsg struct{}

type promptContextDoneMsg struct{}

type promptProgressTickMsg struct{}

type promptStartupDoneMsg struct {
	Username     string
	History      []string
	HistoryLimit int
	Err          error
}

type promptOutputBlockKind uint8

const (
	promptOutputText promptOutputBlockKind = iota
	promptOutputTable
	promptOutputProgress
)

type promptOutputBlock struct {
	kind     promptOutputBlockKind
	text     string
	table    renderer.TableData
	progress renderer.Event
}

var (
	promptHeaderStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	promptSelectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	promptCompletionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	promptHintStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	promptPanelBorder     = lipgloss.NormalBorder()
	promptPanelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

const promptEditorPrefix = "tg> "

const promptBorderedLayoutBaseLines = 1 + 2 + 2 + 2 + 1

type promptLayout struct {
	bordered       bool
	outputRows     int
	suggestionRows int
	commandRows    int
	showHint       bool
}

func newPromptModel(options promptModelOptions) *promptModel {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Lifetime == nil {
		options.Lifetime = context.Background()
	}

	editor := textinput.New()
	editor.Prompt = promptEditorPrefix
	editor.SetWidth(0)
	_ = editor.Focus()

	m := &promptModel{
		ctx:                 options.Context,
		lifetime:            options.Lifetime,
		username:            options.Username,
		version:             options.Version,
		connected:           options.Connected,
		editor:              editor,
		viewport:            viewport.New(),
		complete:            options.Complete,
		submit:              options.Submit,
		events:              options.Events,
		history:             append([]string(nil), options.History...),
		historyIndex:        len(options.History),
		historyLimit:        options.HistoryLimit,
		activeRows:          make(map[string]renderer.Event),
		activeRowObservedAt: make(map[string]time.Time),
		terminalRows:        make(map[string]struct{}),
		pendingDone:         make(map[string]promptCommandDoneMsg),
		barriers:            make(map[string]struct{}),
		completedRuns:       make(map[string]struct{}),
		followBottom:        true,
		state:               promptStateReady,
		startup:             options.Startup,
		startupCancel:       options.StartupCancel,
		authRequests:        options.AuthRequests,
	}
	if options.Startup != nil {
		m.state = promptStateStarting
		m.connected = false
		m.editor.Blur()
	}

	m.viewport.MouseWheelEnabled = true
	m.resize(80, 24)

	return m
}

func (m *promptModel) Init() tea.Cmd {
	return tea.Batch(
		waitForRendererEvent(m.lifetime, m.events),
		waitForPromptContext(m.lifetime, m.ctx),
		waitForAuthCodeRequest(m.lifetime, m.authRequests),
		m.startup,
	)
}

func (m *promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

		return m, nil

	case promptRendererEventMsg:
		if msg.event.Kind == renderer.EventBarrier {
			m.barriers[msg.event.ID] = struct{}{}
			_, cmd := m.finalizeCommand(msg.event.ID)
			if cmd != nil {
				return m, cmd
			}
			return m, waitForRendererEvent(m.lifetime, m.events)
		}

		m.applyRendererEvent(msg.event)

		commands := []tea.Cmd{waitForRendererEvent(m.lifetime, m.events)}
		if !m.progressTicking && m.hasUnknownProgress() {
			m.progressTicking = true
			commands = append(commands, tickPromptProgress())
		}

		return m, tea.Batch(commands...)

	case promptRendererEventsClosedMsg:
		m.events = nil

		return m, nil

	case promptContextDoneMsg:
		if m.state != promptStateReady {
			return m.cancelStartup()
		}
		if m.running {
			m.quitting = true
			m.cancel()
			return m, nil
		}

		return m, tea.Quit

	case promptProgressTickMsg:
		m.progressFrame++

		if m.hasUnknownProgress() {
			return m, tickPromptProgress()
		}

		m.progressTicking = false

		return m, nil

	case promptCommandDoneMsg:
		m.pendingDone[msg.RunID] = msg

		_, cmd := m.finalizeCommand(msg.RunID)

		return m, cmd

	case promptStartupDoneMsg:
		return m.finishStartup(msg)

	case promptAuthCodeRequestMsg:
		return m.beginAuthCodeEntry(msg.Request)

	case promptAuthCodeRequestsClosedMsg:
		m.authRequests = nil

		return m, nil

	case tea.KeyPressMsg:
		return m.updateKey(msg)

	case tea.MouseWheelMsg:
		return m.updateViewport(msg)

	default:
		if m.running {
			return m, nil
		}

		return m.updateEditor(msg)
	}
}

func (m *promptModel) finalizeCommand(runID string) (bool, tea.Cmd) {
	if _, completed := m.completedRuns[runID]; completed {
		return false, nil
	}
	done, hasDone := m.pendingDone[runID]
	_, hasBarrier := m.barriers[runID]
	if !hasDone || !hasBarrier {
		return false, nil
	}

	clear(m.pendingDone)
	clear(m.barriers)
	clear(m.completedRuns)
	m.completedRuns[runID] = struct{}{}
	m.finishCommand(done)

	if m.quitting {
		return true, tea.Quit
	}
	return true, nil
}

func (m promptModel) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "tgdownloader"

	return view
}

func (m *promptModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.state != promptStateReady {
		return m.updateStartupKey(msg)
	}

	if msg.Mod == tea.ModCtrl && (msg.Code == tea.KeyUp || msg.Code == tea.KeyDown) {
		m.syncViewportContent()
		if msg.Code == tea.KeyUp {
			m.viewport.ScrollUp(1)
		} else {
			m.viewport.ScrollDown(1)
		}
		m.followBottom = m.viewport.AtBottom()
		return m, nil
	}

	if msg.Code == tea.KeyPgUp || msg.Code == tea.KeyPgDown {
		if len(m.completions) > 0 {
			m.pageCompletions(msg.Code == tea.KeyPgDown)
			return m, nil
		}
		return m.updateViewport(msg)
	}

	if m.running {
		if msg.Keystroke() == "ctrl+c" && m.cancel != nil {
			m.cancel()
		}
		return m, nil
	}

	if msg.Keystroke() == "ctrl+c" || msg.Keystroke() == "ctrl+d" {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.clearCompletions()
		return m, nil

	case tea.KeyTab:
		if len(m.completions) > 0 {
			m.acceptCompletion()
		}
		return m, nil

	case tea.KeyUp:
		if len(m.completions) > 0 {
			m.selected = (m.selected - 1 + len(m.completions)) % len(m.completions)
			m.ensureCompletionVisible(m.visibleCompletionCount())
		} else {
			m.historyPrevious()
		}
		return m, nil

	case tea.KeyDown:
		if len(m.completions) > 0 {
			m.selected = (m.selected + 1) % len(m.completions)
			m.ensureCompletionVisible(m.visibleCompletionCount())
		} else {
			m.historyNext()
		}
		return m, nil

	case tea.KeyEnter, tea.KeyKpEnter:
		if len(m.completions) > 0 {
			m.acceptCompletion()
			return m, nil
		}
		return m.submitLine()
	}

	return m.updateEditor(msg)
}

func (m *promptModel) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	value, position := m.editor.Value(), m.editor.Position()

	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if m.editor.Value() != value || m.editor.Position() != position {
		m.refreshCompletions()
	}

	return m, cmd
}

func (m *promptModel) submitLine() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.editor.Value())
	if line == "" {
		return m, nil
	}

	if isPromptExitLine(line) {
		return m, tea.Quit
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.running = true
	m.editor.Blur()
	m.clearCompletions()
	m.appendTranscriptText(promptEditorPrefix + line)
	m.syncViewportContent()
	m.editor.SetValue("")

	if m.submit == nil {
		return m, nil
	}
	command := m.submit(ctx, line)
	if command == nil {
		return m, nil
	}

	done := make(chan struct{})
	m.activeCommandDone = done
	return m, func() tea.Msg {
		defer close(done)
		return command()
	}
}

func (m *promptModel) finishCommand(msg promptCommandDoneMsg) {
	if msg.Err != nil {
		if errors.Is(msg.Err, context.Canceled) {
			m.appendTranscriptText("Interrupted")
		} else {
			var rendered strings.Builder
			renderer.RenderErrorConcise(&rendered, msg.Err)
			if text := sanitizePromptModelText(rendered.String()); text != "" {
				m.appendTranscriptText(text)
			}
		}
	}

	m.syncViewportContent()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.activeCommandDone = nil
	m.running = false
	_ = m.editor.Focus()

	if msg.HistoryOK && msg.HistoryStored {
		m.history = append(m.history, msg.Line)
		if m.historyLimit > 0 && len(m.history) > m.historyLimit {
			m.history = m.history[len(m.history)-m.historyLimit:]
		}
	}

	m.historyIndex = len(m.history)
}

func (m *promptModel) refreshCompletions() {
	m.clearCompletions()
	if m.state != promptStateReady || m.complete == nil {
		return
	}
	result := m.complete(m.lifetime, m.editor.Value(), m.editor.Position())
	if result.Err != nil {
		m.completionStatus = sanitizePromptModelText(fmt.Sprintf("Completion: %v", result.Err))
		return
	}

	m.completions = append([]promptCandidate(nil), result.Candidates...)
	m.completionStart = result.Start
	m.completionEnd = result.End
	m.completionQuoted = result.Quoted
	m.completionQuoteClosed = result.QuoteClosed
}

func (m *promptModel) acceptCompletion() {
	if len(m.completions) == 0 || m.selected >= len(m.completions) {
		return
	}

	candidate := m.completions[m.selected]
	runes := []rune(m.editor.Value())
	start := min(max(0, m.completionStart), len(runes))
	end := min(max(start, m.completionEnd), len(runes))
	value := encodePromptCompletionValue(candidate.Value, m.completionQuoted, m.completionQuoteClosed)
	updated := string(runes[:start]) + value + string(runes[end:])

	m.editor.SetValue(updated)
	m.editor.SetCursor(start + len([]rune(value)))
	m.clearCompletions()
}

func (m *promptModel) clearCompletions() {
	m.completions = nil
	m.selected = 0
	m.completionOffset = 0
	m.completionStart = 0
	m.completionEnd = 0
	m.completionQuoted = false
	m.completionQuoteClosed = false
	m.completionStatus = ""
}

func encodePromptCompletionValue(value string, quoted, quoteClosed bool) string {
	value = strings.ReplaceAll(value, `"`, `""`)
	if quoted {
		if !quoteClosed {
			return value + `"`
		}
		return value
	}
	if strings.ContainsAny(value, " \t\"") {
		return `"` + value + `"`
	}
	return value
}

func (m *promptModel) visibleCompletions() []promptCandidate {
	count := m.visibleCompletionCount()
	if count == 0 || len(m.completions) == 0 {
		return nil
	}

	m.ensureCompletionVisible(count)
	end := min(m.completionOffset+count, len(m.completions))
	return m.completions[m.completionOffset:end]
}

func (m *promptModel) visibleCompletionCount() int {
	return promptLayoutForHeight(m.height).suggestionRows
}

func (m *promptModel) ensureCompletionVisible(count int) {
	if count <= 0 || len(m.completions) == 0 {
		return
	}

	m.selected = min(max(0, m.selected), len(m.completions)-1)
	if m.selected < m.completionOffset {
		m.completionOffset = m.selected
	} else if m.selected >= m.completionOffset+count {
		m.completionOffset = m.selected - count + 1
	}
	m.completionOffset = min(max(0, m.completionOffset), max(0, len(m.completions)-count))
}

func (m *promptModel) pageCompletions(down bool) {
	count := m.visibleCompletionCount()
	if count == 0 {
		return
	}

	if down {
		m.selected = (m.selected + count) % len(m.completions)
	} else {
		m.selected = ((m.selected-count)%len(m.completions) + len(m.completions)) % len(m.completions)
	}
	m.ensureCompletionVisible(count)
}

func (m *promptModel) historyPrevious() {
	if len(m.history) == 0 || m.historyIndex == 0 {
		return
	}

	m.historyIndex--
	m.editor.SetValue(m.history[m.historyIndex])
	m.editor.CursorEnd()
}

func (m *promptModel) historyNext() {
	if len(m.history) == 0 || m.historyIndex >= len(m.history) {
		return
	}

	m.historyIndex++
	if m.historyIndex == len(m.history) {
		m.editor.SetValue("")
		return
	}

	m.editor.SetValue(m.history[m.historyIndex])
	m.editor.CursorEnd()
}

func (m *promptModel) resize(width, height int) {
	m.width = max(1, width)
	m.height = max(1, height)

	contentWidth := m.width
	if promptLayoutForHeight(m.height).bordered {
		contentWidth = promptPanelBodyWidth(m.width)
	}
	m.editor.Prompt = promptSingleLine(m.editorPrefix(), max(0, contentWidth-1))
	m.editor.SetWidth(max(0, contentWidth-lipgloss.Width(m.editor.Prompt)-1))
	m.viewport.SetWidth(contentWidth)
	m.viewport.SoftWrap = true
	m.viewport.FillHeight = true
	m.syncViewportContent()
}

func (m *promptModel) applyRendererEvent(event renderer.Event) {
	event.Text = sanitizePromptModelLine(event.Text)
	event.Label = sanitizePromptModelLine(event.Label)
	if event.Kind == renderer.EventTable {
		if event.Table != nil {
			m.appendTranscriptTable(sanitizePromptTable(*event.Table))
			m.syncViewportContent()
		}
		return
	}
	if event.Label == "" && event.Kind != renderer.EventLine {
		event.Label = event.Text
	}

	if event.Kind == renderer.EventLine || event.ID == "" {
		if event.Text != "" {
			m.appendTranscriptText(event.Text)
			m.syncViewportContent()
		}
		return
	}
	if _, terminal := m.terminalRows[event.ID]; terminal {
		return
	}

	switch event.Kind {
	case renderer.EventProgressDone, renderer.EventProgressFail:
		delete(m.activeRows, event.ID)
		delete(m.activeRowObservedAt, event.ID)
		m.removeActiveRowID(event.ID)
		m.terminalRows[event.ID] = struct{}{}
		if event.Label != "" {
			m.appendTranscriptProgress(event)
		}
		m.syncViewportContent()
	case renderer.EventProgressCreate, renderer.EventProgressUpdate, "":
		if _, exists := m.activeRows[event.ID]; !exists {
			m.activeRowOrder = append(m.activeRowOrder, event.ID)
		}
		m.activeRows[event.ID] = event
		m.activeRowObservedAt[event.ID] = time.Now()
		m.syncViewportContent()
	}
}

func (m *promptModel) removeActiveRowID(id string) {
	for i, activeID := range m.activeRowOrder {
		if activeID == id {
			m.activeRowOrder = append(m.activeRowOrder[:i], m.activeRowOrder[i+1:]...)
			return
		}
	}
}

func (m *promptModel) render() string {
	connection := "disconnected"
	if m.connected {
		connection = "connected"
	}

	header := promptHeaderStyle.Render(promptSingleLine(fmt.Sprintf("tgdownloader  %s  %s  %s", m.username, connection, m.version), m.width))

	m.syncViewportContent()
	layout := promptLayoutForHeight(m.height)
	if !layout.bordered {
		return m.renderCompact(header, layout)
	}

	lines := []string{header}
	lines = append(lines, renderPromptPanel("OUTPUT", m.outputBody(layout.outputRows), m.width)...)

	visible := m.visibleCompletions()
	suggestions := make([]string, layout.suggestionRows)
	contentWidth := promptPanelBodyWidth(m.width)
	for i := 0; i < layout.suggestionRows; i++ {
		if i == 0 && m.completionStatus != "" {
			suggestions[i] = promptHintStyle.Render(promptSingleLine(m.completionStatus, contentWidth))
			continue
		}
		if i >= len(visible) {
			continue
		}
		prefix := "  "
		if i == m.selected-m.completionOffset {
			prefix = "> "
		}
		text := formatPromptCandidate(visible[i], max(0, contentWidth-lipgloss.Width(prefix)))
		if i == m.selected-m.completionOffset {
			suggestions[i] = promptSelectedStyle.Render(prefix + text)
		} else {
			suggestions[i] = promptCompletionStyle.Render(prefix + text)
		}
	}
	lines = append(lines, renderPromptPanel(m.suggestionPanelTitle(), suggestions, m.width)...)

	command := make([]string, layout.commandRows)
	if layout.commandRows > 0 && (m.state == promptStateReady || m.state == promptStateAuth) {
		command[0] = m.editor.View()
	}
	lines = append(lines, renderPromptPanel("COMMAND", command, m.width)...)

	if layout.showHint {
		lines = append(lines, m.renderHint())
	}
	return strings.Join(lines, "\n")
}

func (m *promptModel) renderCompact(header string, layout promptLayout) string {
	lines := []string{header}
	lines = append(lines, m.outputBody(layout.outputRows)...)
	if layout.commandRows > 0 {
		lines = append(lines, m.editor.View())
	}
	if layout.showHint {
		lines = append(lines, m.renderHint())
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:m.height], "\n")
}

func (m *promptModel) outputBody(rows int) []string {
	body := make([]string, 0, rows)
	if m.viewport.Height() > 0 {
		body = append(body, strings.Split(m.viewport.View(), "\n")...)
	}

	contentWidth := m.viewport.Width()
	for _, id := range m.visibleActiveRowIDs() {
		body = append(body, m.renderProgressRow(m.activeRows[id], contentWidth))
	}

	if len(body) > rows {
		body = body[:rows]
	}
	for len(body) < rows {
		body = append(body, "")
	}
	return body
}

func (m *promptModel) renderProgressRow(event renderer.Event, width int) string {
	if observedAt, ok := m.activeRowObservedAt[event.ID]; ok {
		event.Elapsed += time.Since(observedAt)
	}
	return renderer.FormatProgress(event, width, m.progressFrame)
}

func (m *promptModel) hasUnknownProgress() bool {
	for _, event := range m.activeRows {
		if event.Total <= 0 {
			return true
		}
	}
	return false
}

func tickPromptProgress() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return promptProgressTickMsg{}
	})
}

func (m *promptModel) suggestionPanelTitle() string {
	total := len(m.completions)
	current := 0
	if total > 0 {
		current = min(max(0, m.selected), total-1) + 1
	}

	return fmt.Sprintf("SUGGESTIONS %d/%d", current, total)
}

func (m *promptModel) renderHint() string {
	hint := "enter run  up/down history  ctrl+up/down scroll  ctrl+c exit"
	if m.state == promptStateAuth {
		hint = "enter submit  ctrl+c cancel"
	} else if m.state == promptStateStarting || m.state == promptStateStopping {
		hint = "ctrl+c cancel"
	} else if m.state == promptStateFailed {
		hint = "ctrl+c exit"
	} else if m.running {
		hint = "ctrl+c cancel  ctrl+up/down scroll"
	} else if len(m.completions) > 0 {
		hint = "up/down select  tab/enter insert  esc close  ctrl+up/down scroll"
	}
	return promptHintStyle.Render(promptSingleLine(hint, m.width))
}

func (m *promptModel) editorPrefix() string {
	if m.state == promptStateAuth {
		return "code> "
	}
	return promptEditorPrefix
}

func (m *promptModel) beginAuthCodeEntry(request *tuiAuthCodeRequest) (tea.Model, tea.Cmd) {
	if request == nil || m.state == promptStateStopping || m.state == promptStateFailed {
		return m, nil
	}

	m.state = promptStateAuth
	m.authRequest = request

	m.clearCompletions()
	m.editor.SetValue("")
	m.editor.EchoMode = textinput.EchoPassword
	m.editor.EchoCharacter = '*'
	m.editor.Prompt = "code> "
	_ = m.editor.Focus()

	m.resize(m.width, m.height)
	return m, nil
}

func (m *promptModel) updateStartupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Keystroke() == "ctrl+c" || msg.Keystroke() == "ctrl+d" {
		if m.state == promptStateFailed {
			return m, tea.Quit
		}
		return m.cancelStartup()
	}

	if m.state != promptStateAuth {
		return m, nil
	}
	if msg.Code == tea.KeyEnter || msg.Code == tea.KeyKpEnter {
		code := strings.TrimSpace(m.editor.Value())

		m.editor.SetValue("")
		m.editor.EchoMode = textinput.EchoNormal
		m.editor.Prompt = promptEditorPrefix
		m.editor.Blur()

		request := m.authRequest
		m.authRequest = nil
		m.state = promptStateStarting

		if request != nil {
			request.Respond(code, nil)
		}

		m.resize(m.width, m.height)
		return m, waitForAuthCodeRequest(m.lifetime, m.authRequests)
	}

	return m.updateEditor(msg)
}

func (m *promptModel) cancelStartup() (tea.Model, tea.Cmd) {
	if m.state == promptStateFailed {
		return m, tea.Quit
	}

	m.quitting = true
	m.state = promptStateStopping

	if m.authRequest != nil {
		m.authRequest.Respond("", context.Canceled)
		m.authRequest = nil
	}

	if m.startupCancel != nil {
		m.startupCancel()
		m.startupCancel = nil
	}
	m.editor.SetValue("")
	m.editor.Blur()

	return m, nil
}

func (m *promptModel) finishStartup(msg promptStartupDoneMsg) (tea.Model, tea.Cmd) {
	m.startup = nil
	m.startupErr = msg.Err

	if m.quitting || m.state == promptStateStopping {
		return m, tea.Quit
	}
	if msg.Err != nil {
		m.state = promptStateFailed
		m.editor.SetValue("")
		m.editor.Blur()

		var rendered strings.Builder
		renderer.RenderErrorConcise(&rendered, msg.Err)
		if value := sanitizePromptModelText(rendered.String()); value != "" {
			m.appendTranscriptText(value)
			m.syncViewportContent()
		}

		return m, nil
	}

	m.state = promptStateReady
	m.connected = true
	m.username = sanitizePromptModelLine(msg.Username)

	m.history = append([]string(nil), msg.History...)
	m.historyIndex = len(m.history)
	m.historyLimit = msg.HistoryLimit

	m.editor.EchoMode = textinput.EchoNormal
	m.editor.Prompt = promptEditorPrefix
	m.editor.SetValue("")
	_ = m.editor.Focus()

	m.resize(m.width, m.height)
	return m, nil
}

func renderPromptPanel(title string, body []string, width int) []string {
	width = max(1, width)
	if width == 1 {
		lines := []string{promptPanelStyle.Render(promptSingleLine(promptPanelBorder.TopLeft, width))}
		for _, line := range body {
			lines = append(lines, promptPanelLine(line, width))
		}
		return append(lines, promptPanelStyle.Render(promptSingleLine(promptPanelBorder.BottomLeft, width)))
	}

	bodyWidth := promptPanelBodyWidth(width)
	topContent := strings.Repeat(promptPanelBorder.Top, bodyWidth)
	if bodyWidth > 1 {
		label := promptSingleLine(" "+sanitizePromptModelText(title)+" ", bodyWidth-1)
		topContent = promptPanelBorder.Top + label + strings.Repeat(promptPanelBorder.Top, bodyWidth-1-lipgloss.Width(label))
	}
	lines := []string{promptPanelStyle.Render(promptPanelBorder.TopLeft + topContent + promptPanelBorder.TopRight)}
	for _, line := range body {
		lines = append(lines,
			promptPanelStyle.Render(promptPanelBorder.Left)+
				promptPanelLine(line, bodyWidth)+
				promptPanelStyle.Render(promptPanelBorder.Right),
		)
	}
	bottom := promptPanelBorder.BottomLeft + strings.Repeat(promptPanelBorder.Bottom, bodyWidth) + promptPanelBorder.BottomRight
	return append(lines, promptPanelStyle.Render(bottom))
}

func promptPanelLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) > width {
		line = promptSingleLine(line, width)
	}

	return line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
}

func promptPanelBodyWidth(width int) int {
	return max(0, width-lipgloss.Width(promptPanelBorder.Left)-lipgloss.Width(promptPanelBorder.Right))
}

func promptLayoutForHeight(height int) promptLayout {
	height = max(1, height)
	if height < promptBorderedLayoutBaseLines+2 {
		layout := promptLayout{outputRows: height - 1}
		if height >= 2 {
			layout.commandRows = 1
			layout.outputRows--
		}
		if height >= 3 {
			layout.showHint = true
			layout.outputRows--
		}
		layout.outputRows = max(0, layout.outputRows)

		return layout
	}

	layout := promptLayout{bordered: true, showHint: true}
	remaining := height - promptBorderedLayoutBaseLines
	layout.commandRows = min(1, remaining)
	remaining -= layout.commandRows
	layout.outputRows = min(1, remaining)
	remaining -= layout.outputRows
	layout.suggestionRows = min(maxPromptVisibleCompletions, remaining)
	remaining -= layout.suggestionRows
	layout.outputRows += remaining
	return layout
}

func (m *promptModel) resizeViewport() {
	m.viewport.SetHeight(max(0, m.contentHeight()-len(m.visibleActiveRowIDs())))
}

func (m *promptModel) syncViewportContent() {
	m.resizeViewport()
	content := strings.Join(m.transcript, "\n")
	if len(m.outputBlocks) > 0 {
		content = strings.Join(m.renderOutputBlocks(m.viewport.Width()), "\n")
	}

	m.viewport.SetContent(content)
	if m.followBottom {
		m.viewport.GotoBottom()
	}
}

func (m *promptModel) appendTranscriptText(value string) {
	m.ensureOutputBlocks()
	m.transcript = append(m.transcript, value)
	m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputText, text: value})
}

func (m *promptModel) appendTranscriptTable(data renderer.TableData) {
	m.ensureOutputBlocks()
	m.transcript = append(m.transcript, strings.Join(renderer.FormatTable(data, m.viewport.Width()), "\n"))
	m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputTable, table: renderer.CloneTableData(data)})
}

func (m *promptModel) appendTranscriptProgress(event renderer.Event) {
	m.ensureOutputBlocks()
	m.transcript = append(m.transcript, renderer.FormatProgress(event, m.viewport.Width(), m.progressFrame))
	m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputProgress, progress: event})
}

func (m *promptModel) ensureOutputBlocks() {
	if len(m.outputBlocks) > 0 || len(m.transcript) == 0 {
		return
	}

	for _, text := range m.transcript {
		m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputText, text: text})
	}
}

func (m *promptModel) renderOutputBlocks(width int) []string {
	var lines []string
	for _, block := range m.outputBlocks {
		switch block.kind {
		case promptOutputTable:
			lines = append(lines, renderer.FormatTable(block.table, width)...)
		case promptOutputProgress:
			lines = append(lines, renderer.FormatProgress(block.progress, width, m.progressFrame))
		default:
			lines = append(lines, strings.Split(block.text, "\n")...)
		}
	}

	return lines
}

func sanitizePromptTable(data renderer.TableData) renderer.TableData {
	data = renderer.CloneTableData(data)
	for i := range data.Columns {
		data.Columns[i].Header = sanitizePromptModelText(data.Columns[i].Header)
	}

	for i := range data.Rows {
		for j := range data.Rows[i] {
			data.Rows[i][j] = sanitizePromptModelLine(data.Rows[i][j])
		}
	}

	return data
}

func (m *promptModel) updateViewport(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncViewportContent()
	before := m.viewport.YOffset()
	updated, cmd := m.viewport.Update(msg)
	m.viewport = updated

	if m.viewport.YOffset() != before {
		m.followBottom = m.viewport.AtBottom()
	} else if m.viewport.AtBottom() {
		m.followBottom = true
	}

	return m, cmd
}

func (m *promptModel) contentHeight() int {
	return promptLayoutForHeight(m.height).outputRows
}

func (m *promptModel) visibleActiveRowIDs() []string {
	return m.activeRowOrder[:min(len(m.activeRowOrder), m.contentHeight())]
}

func promptSingleLine(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return truncatePromptText(sanitizePromptModelText(value), width)
}

func waitForRendererEvent(lifetime context.Context, events <-chan renderer.Event) tea.Cmd {
	if events == nil {
		return nil
	}

	if lifetime == nil {
		lifetime = context.Background()
	}

	return func() tea.Msg {
		select {
		case event, ok := <-events:
			if !ok {
				return promptRendererEventsClosedMsg{}
			}
			return promptRendererEventMsg{event: event}
		case <-lifetime.Done():
			return promptRendererEventsClosedMsg{}
		}
	}
}

func waitForPromptContext(lifetime, ctx context.Context) tea.Cmd {
	if ctx == nil || ctx.Done() == nil {
		return nil
	}

	if lifetime == nil {
		lifetime = context.Background()
	}

	return func() tea.Msg {
		select {
		case <-ctx.Done():
		case <-lifetime.Done():
		}
		return promptContextDoneMsg{}
	}
}

func isPromptExitLine(line string) bool {
	args, err := splitPromptLine(line)
	return err == nil && len(args) == 1 && args[0] == "exit"
}
