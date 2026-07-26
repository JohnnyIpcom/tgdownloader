package cmd

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

type oneShotExecuteFunc func(context.Context) tea.Cmd

type oneShotModelOptions struct {
	Context       context.Context
	Lifetime      context.Context
	Events        <-chan renderer.Event
	Startup       tea.Cmd
	StartupCancel context.CancelFunc
	AuthRequests  <-chan *tuiAuthCodeRequest
	Execute       oneShotExecuteFunc
}

type oneShotStartupDoneMsg struct {
	Err error
}

type oneShotModel struct {
	width int

	ctx      context.Context
	lifetime context.Context
	events   <-chan renderer.Event
	startup  tea.Cmd
	execute  oneShotExecuteFunc
	editor   textinput.Model
	state    promptState

	startupCancel context.CancelFunc
	commandCancel context.CancelFunc
	authRequests  <-chan *tuiAuthCodeRequest
	authRequest   *tuiAuthCodeRequest

	outputBlocks        []promptOutputBlock
	activeRows          map[string]renderer.Event
	activeRowOrder      []string
	activeRowObservedAt map[string]time.Time
	terminalRows        map[string]struct{}
	pendingDone         map[string]promptCommandDoneMsg
	barriers            map[string]struct{}

	progressFrame   int
	progressTicking bool
	preparing       bool
	preparingAt     time.Time
	running         bool
	quitting        bool
	startupErr      error
	commandErr      error
}

func newOneShotModel(options oneShotModelOptions) *oneShotModel {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Lifetime == nil {
		options.Lifetime = context.Background()
	}

	editor := textinput.New()
	editor.Prompt = "code> "
	editor.EchoMode = textinput.EchoPassword
	editor.EchoCharacter = '*'
	editor.Blur()

	return &oneShotModel{
		width:               80,
		ctx:                 options.Context,
		lifetime:            options.Lifetime,
		events:              options.Events,
		startup:             options.Startup,
		execute:             options.Execute,
		editor:              editor,
		state:               promptStateStarting,
		startupCancel:       options.StartupCancel,
		authRequests:        options.AuthRequests,
		activeRows:          make(map[string]renderer.Event),
		activeRowObservedAt: make(map[string]time.Time),
		terminalRows:        make(map[string]struct{}),
		pendingDone:         make(map[string]promptCommandDoneMsg),
		barriers:            make(map[string]struct{}),
	}
}

func (m *oneShotModel) Init() tea.Cmd {
	return tea.Batch(
		waitForRendererEvent(m.lifetime, m.events),
		waitForPromptContext(m.lifetime, m.ctx),
		waitForAuthCodeRequest(m.lifetime, m.authRequests),
		m.startup,
	)
}

func (m *oneShotModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.editor.SetWidth(max(0, m.width-len("code> ")))
		return m, nil

	case promptRendererEventMsg:
		if msg.event.Kind == renderer.EventBarrier {
			m.barriers[msg.event.ID] = struct{}{}
			return m, m.finalizeCommand(msg.event.ID)
		}

		m.applyRendererEvent(msg.event)
		commands := []tea.Cmd{waitForRendererEvent(m.lifetime, m.events)}
		if !m.progressTicking && m.hasUnknownProgress() {
			m.progressTicking = true
			commands = append(commands, tickOneShotProgress())
		}
		return m, tea.Batch(commands...)

	case promptRendererEventsClosedMsg:
		m.events = nil
		return m, nil

	case oneShotStartupDoneMsg:
		return m.finishStartup(msg)

	case promptCommandDoneMsg:
		m.pendingDone[msg.RunID] = msg
		return m, m.finalizeCommand(msg.RunID)

	case promptAuthCodeRequestMsg:
		return m.beginAuthCodeEntry(msg.Request)

	case promptAuthCodeRequestsClosedMsg:
		m.authRequests = nil
		return m, nil

	case promptProgressTickMsg:
		m.progressFrame++
		if m.hasUnknownProgress() {
			return m, tickOneShotProgress()
		}

		m.progressTicking = false
		return m, nil

	case promptContextDoneMsg:
		return m.cancelWork()

	case tea.KeyPressMsg:
		return m.updateKey(msg)

	default:
		if m.state != promptStateAuth {
			return m, nil
		}

		var command tea.Cmd
		m.editor, command = m.editor.Update(msg)
		return m, command
	}
}

func (m *oneShotModel) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = false
	view.WindowTitle = "tgdownloader"
	return view
}

func (m *oneShotModel) finishStartup(msg oneShotStartupDoneMsg) (tea.Model, tea.Cmd) {
	m.startup = nil
	m.startupErr = msg.Err

	if m.quitting || m.state == promptStateStopping {
		return m, tea.Quit
	}
	if msg.Err != nil {
		m.state = promptStateFailed
		m.appendError(msg.Err)
		return m, nil
	}

	m.state = promptStateReady
	if m.execute == nil {
		return m, tea.Quit
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.commandCancel = cancel
	m.running = true
	m.preparing = true
	m.preparingAt = time.Now()
	m.progressTicking = true

	return m, tea.Batch(m.execute(ctx), tickOneShotProgress())
}

func (m *oneShotModel) beginAuthCodeEntry(request *tuiAuthCodeRequest) (tea.Model, tea.Cmd) {
	if request == nil || m.state == promptStateStopping || m.state == promptStateFailed {
		return m, nil
	}

	m.state = promptStateAuth
	m.authRequest = request
	m.editor.SetValue("")
	_ = m.editor.Focus()

	return m, nil
}

func (m *oneShotModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Keystroke() == "ctrl+c" || msg.Keystroke() == "ctrl+d" {
		if m.state == promptStateFailed {
			return m, tea.Quit
		}

		return m.cancelWork()
	}
	if m.state != promptStateAuth {
		return m, nil
	}
	if msg.Code == tea.KeyEnter || msg.Code == tea.KeyKpEnter {
		code := strings.TrimSpace(m.editor.Value())
		request := m.authRequest

		m.authRequest = nil
		m.state = promptStateStarting
		m.editor.SetValue("")
		m.editor.Blur()

		if request != nil {
			request.Respond(code, nil)
		}

		return m, waitForAuthCodeRequest(m.lifetime, m.authRequests)
	}

	var command tea.Cmd
	m.editor, command = m.editor.Update(msg)
	return m, command
}

func (m *oneShotModel) cancelWork() (tea.Model, tea.Cmd) {
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
	if m.commandCancel != nil {
		m.commandCancel()
		m.commandCancel = nil
	}

	if !m.running && m.startup == nil {
		return m, tea.Quit
	}

	return m, nil
}

func (m *oneShotModel) finalizeCommand(runID string) tea.Cmd {
	done, hasDone := m.pendingDone[runID]
	_, hasBarrier := m.barriers[runID]
	if !hasDone || !hasBarrier {
		return nil
	}

	delete(m.pendingDone, runID)
	delete(m.barriers, runID)
	m.running = false
	m.preparing = false
	m.commandErr = done.Err

	if m.commandCancel != nil {
		m.commandCancel()
		m.commandCancel = nil
	}
	if done.Err != nil {
		if errors.Is(done.Err, context.Canceled) {
			m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputText, text: "Interrupted"})
		} else {
			m.appendError(done.Err)
		}
	}

	return tea.Quit
}

func (m *oneShotModel) applyRendererEvent(event renderer.Event) {
	m.preparing = false
	event.Text = sanitizePromptModelLine(event.Text)
	event.Label = sanitizePromptModelLine(event.Label)

	switch event.Kind {
	case renderer.EventLine:
		if event.Text != "" {
			m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputText, text: event.Text})
		}

	case renderer.EventTable:
		if event.Table != nil {
			m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputTable, table: sanitizePromptTable(*event.Table)})
		}

	case renderer.EventProgressDone, renderer.EventProgressFail:
		if _, terminal := m.terminalRows[event.ID]; terminal {
			return
		}

		delete(m.activeRows, event.ID)
		delete(m.activeRowObservedAt, event.ID)
		m.removeActiveRowID(event.ID)
		m.terminalRows[event.ID] = struct{}{}
		m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputProgress, progress: event})

	case renderer.EventProgressCreate, renderer.EventProgressUpdate, "":
		if _, terminal := m.terminalRows[event.ID]; terminal {
			return
		}
		if _, exists := m.activeRows[event.ID]; !exists {
			m.activeRowOrder = append(m.activeRowOrder, event.ID)
		}

		m.activeRows[event.ID] = event
		m.activeRowObservedAt[event.ID] = time.Now()
	}
}

func (m *oneShotModel) removeActiveRowID(id string) {
	for index, activeID := range m.activeRowOrder {
		if activeID == id {
			m.activeRowOrder = append(m.activeRowOrder[:index], m.activeRowOrder[index+1:]...)
			return
		}
	}
}

func (m *oneShotModel) appendError(err error) {
	var rendered strings.Builder
	renderer.RenderErrorConcise(&rendered, err)

	if text := sanitizePromptModelText(rendered.String()); text != "" {
		m.outputBlocks = append(m.outputBlocks, promptOutputBlock{kind: promptOutputText, text: text})
	}
}

func (m *oneShotModel) render() string {
	lines := renderOneShotOutputBlocks(m.outputBlocks, m.width, m.progressFrame)
	if m.preparing {
		lines = append(lines, renderer.FormatProgress(renderer.Event{
			Label:   "Preparing command",
			Elapsed: time.Since(m.preparingAt),
		}, m.width, m.progressFrame))
	}

	for _, id := range m.activeRowOrder {
		event := m.activeRows[id]
		if observedAt, ok := m.activeRowObservedAt[id]; ok {
			event.Elapsed += time.Since(observedAt)
		}

		lines = append(lines, renderer.FormatProgress(event, m.width, m.progressFrame))
	}
	if m.state == promptStateAuth {
		lines = append(lines, m.editor.View())
	}

	return strings.Join(lines, "\n")
}

func renderOneShotOutputBlocks(blocks []promptOutputBlock, width, frame int) []string {
	var lines []string

	for _, block := range blocks {
		switch block.kind {
		case promptOutputTable:
			lines = append(lines, renderer.FormatTable(block.table, width)...)
		case promptOutputProgress:
			lines = append(lines, renderer.FormatProgress(block.progress, width, frame))
		default:
			lines = append(lines, strings.Split(ansi.Wrap(block.text, max(1, width), " "), "\n")...)
		}
	}

	return lines
}

func (m *oneShotModel) hasUnknownProgress() bool {
	if m.preparing {
		return true
	}

	for _, event := range m.activeRows {
		if event.Total <= 0 {
			return true
		}
	}

	return false
}

func tickOneShotProgress() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return promptProgressTickMsg{}
	})
}
