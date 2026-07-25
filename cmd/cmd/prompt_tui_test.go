package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestPromptTUIRestoresEditorAfterCommand(t *testing.T) {
	r := rootWithPromptCommand("success", func(*cobra.Command, []string) error { return nil })
	m := newIntegratedPromptModel(r, nil)
	m.editor.SetValue("success")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	if !m.running {
		t.Fatal("command did not enter running state")
	}

	m, _ = updatePromptCommandAndDrain(t, m, cmd())
	if m.running || !m.editor.Focused() || m.editor.Value() != "" {
		t.Fatalf("model was not restored: running=%v focused=%v value=%q", m.running, m.editor.Focused(), m.editor.Value())
	}
}

func TestPromptTUIRendersWrappedExpectedCommandErrorOnceAndConcise(t *testing.T) {
	r := rootWithPromptCommand("fail", func(*cobra.Command, []string) error {
		return apperr.New("cmd.test.internal", apperr.KindNetwork, errors.New("expected failure"))
	})
	m := newIntegratedPromptModel(r, nil)
	m.editor.SetValue("fail")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	m, _ = updatePromptCommandAndDrain(t, m, cmd())

	view := m.render()
	if got := strings.Count(view, "Error: expected failure"); got != 1 {
		t.Fatalf("concise error count = %d, want 1: %q", got, view)
	}
	if strings.Contains(view, "cmd.test.internal") || strings.Contains(view, string(apperr.KindNetwork)) {
		t.Fatalf("internal error details leaked into transcript: %q", view)
	}
}

func TestPromptTUICtrlCCancelsActiveCommandAndRestoresEditor(t *testing.T) {
	started := make(chan struct{})
	r := rootWithPromptCommand("wait", func(cmd *cobra.Command, _ []string) error {
		close(started)
		<-cmd.Context().Done()
		return cmd.Context().Err()
	})
	m := newIntegratedPromptModel(r, nil)
	m.editor.SetValue("wait")

	updated, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	done := make(chan tea.Msg, 1)
	go func() { done <- command() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not start")
	}

	updated, quit := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*promptModel)
	if quit != nil {
		t.Fatal("active ctrl+c quit the prompt")
	}

	select {
	case msg := <-done:
		m, _ = updatePromptCommandAndDrain(t, m, msg)
	case <-time.After(time.Second):
		t.Fatal("canceled command did not return")
	}
	if m.running || !m.editor.Focused() {
		t.Fatalf("model was not restored: running=%v focused=%v", m.running, m.editor.Focused())
	}
	if got := m.transcript[len(m.transcript)-1]; got != "Interrupted" {
		t.Fatalf("last transcript line = %q, want Interrupted", got)
	}
}

func TestPromptTUIParentCancellationDrainsEventsUntilCommandDoneThenQuits(t *testing.T) {
	const runID = "parent-canceled"
	parent, cancelParent := context.WithCancel(context.Background())
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	events := make(chan renderer.Event, 1)
	commandReturned := make(chan tea.Msg, 1)
	m := newPromptModel(promptModelOptions{
		Context:  parent,
		Lifetime: lifetime,
		Events:   events,
		Submit: func(ctx context.Context, line string) tea.Cmd {
			return func() tea.Msg {
				<-ctx.Done()
				events <- renderer.Event{Kind: renderer.EventLine, Text: "cleanup output"}
				events <- renderer.Event{Kind: renderer.EventBarrier, ID: runID}
				return promptCommandDoneMsg{RunID: runID, Line: line, Err: ctx.Err()}
			}
		},
	})
	m.editor.SetValue("wait")
	updated, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	go func() { commandReturned <- command() }()

	cancelParent()
	updated, quit := m.Update(waitForPromptContext(lifetime, parent)())
	m = updated.(*promptModel)
	if quit != nil || !m.running || !m.quitting {
		t.Fatalf("parent cancellation quit early: cmd=%v running=%v quitting=%v", quit != nil, m.running, m.quitting)
	}

	updated, _ = m.Update(waitForRendererEvent(lifetime, events)())
	m = updated.(*promptModel)
	if got := m.transcript[len(m.transcript)-1]; got != "cleanup output" {
		t.Fatalf("last transcript line = %q, want cleanup output", got)
	}

	select {
	case msg := <-commandReturned:
		updated, quit = m.Update(msg)
		m = updated.(*promptModel)
	case <-time.After(time.Second):
		t.Fatal("canceled command did not return")
	}
	if !m.running || quit != nil {
		t.Fatalf("done finalized before queued barrier: running=%v quit=%v", m.running, quit != nil)
	}
	updated, quit = m.Update(waitForRendererEvent(lifetime, events)())
	m = updated.(*promptModel)
	if m.running || quit == nil {
		t.Fatalf("command completion did not quit: running=%v quit=%v", m.running, quit != nil)
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("command completion did not return tea.QuitMsg")
	}
}

func TestPromptTUIDoneBeforeCleanupBarrierKeepsEditorBlocked(t *testing.T) {
	const runID = "run-current"
	events := make(chan renderer.Event, 3)
	m := newPromptModel(promptModelOptions{Events: events})
	m.running = true
	m.editor.Blur()

	updated, cmd := m.Update(promptCommandDoneMsg{RunID: runID, Line: "capture"})
	m = updated.(*promptModel)
	if cmd != nil || !m.running || m.editor.Focused() {
		t.Fatalf("done finalized before barrier: cmd=%v running=%v focused=%v", cmd != nil, m.running, m.editor.Focused())
	}

	events <- renderer.Event{Kind: renderer.EventBarrier, ID: "run-stale"}
	events <- renderer.Event{Kind: renderer.EventLine, Text: "cleanup output"}
	events <- renderer.Event{Kind: renderer.EventBarrier, ID: runID}
	for range 3 {
		updated, cmd = m.Update(waitForRendererEvent(context.Background(), events)())
		m = updated.(*promptModel)
		if !m.running && len(m.transcript) == 0 {
			t.Fatal("stale barrier finalized current command")
		}
	}

	if m.running || !m.editor.Focused() {
		t.Fatalf("matching barrier did not restore editor: running=%v focused=%v", m.running, m.editor.Focused())
	}
	if got := m.transcript; !reflect.DeepEqual(got, []string{"cleanup output"}) {
		t.Fatalf("transcript = %q, want cleanup before unblock", got)
	}
}

func TestPromptTUIDoneBeforeCleanupBarrierAppliesCleanupBeforeQuit(t *testing.T) {
	const runID = "run-canceled"
	events := make(chan renderer.Event, 2)
	m := newPromptModel(promptModelOptions{Events: events})
	m.running = true
	m.quitting = true
	m.editor.Blur()

	updated, cmd := m.Update(promptCommandDoneMsg{RunID: runID, Line: "wait", Err: context.Canceled})
	m = updated.(*promptModel)
	if cmd != nil || !m.running {
		t.Fatalf("done quit before barrier: cmd=%v running=%v", cmd != nil, m.running)
	}

	events <- renderer.Event{Kind: renderer.EventLine, Text: "cleanup output"}
	events <- renderer.Event{Kind: renderer.EventBarrier, ID: runID}
	updated, cmd = m.Update(waitForRendererEvent(context.Background(), events)())
	m = updated.(*promptModel)
	if cmd == nil || !m.running {
		t.Fatalf("cleanup event changed running state: cmd=%v running=%v", cmd != nil, m.running)
	}
	updated, cmd = m.Update(waitForRendererEvent(context.Background(), events)())
	m = updated.(*promptModel)
	if m.running || cmd == nil {
		t.Fatalf("barrier did not finalize canceled command: running=%v cmd=%v", m.running, cmd != nil)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("matching barrier did not return tea.QuitMsg")
	}
	if got := m.transcript; !reflect.DeepEqual(got, []string{"cleanup output", "Interrupted"}) {
		t.Fatalf("transcript = %q, want cleanup before Interrupted", got)
	}
}

func TestPromptTUIBarrierBeforeDoneFinalizesWhenDoneArrives(t *testing.T) {
	const runID = "barrier-first"
	m := newPromptModel(promptModelOptions{})
	m.running = true
	m.editor.Blur()

	updated, cmd := m.Update(promptRendererEventMsg{event: renderer.Event{Kind: renderer.EventBarrier, ID: runID}})
	m = updated.(*promptModel)
	if !m.running || m.editor.Focused() {
		t.Fatalf("barrier finalized before done: running=%v focused=%v", m.running, m.editor.Focused())
	}
	updated, cmd = m.Update(promptCommandDoneMsg{RunID: runID, Line: "capture"})
	m = updated.(*promptModel)
	if cmd != nil || m.running || !m.editor.Focused() {
		t.Fatalf("done did not finalize acknowledged run: cmd=%v running=%v focused=%v", cmd != nil, m.running, m.editor.Focused())
	}
}

func TestPromptTUIIdleQuitClosesCleanly(t *testing.T) {
	m := newIntegratedPromptModel(&Root{}, nil)
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: 'd', Mod: tea.ModCtrl},
	} {
		updated, cmd := m.Update(msg)
		m = updated.(*promptModel)
		if cmd == nil {
			t.Fatalf("key %q did not return a quit command", msg.Keystroke())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("key %q command did not return tea.QuitMsg", msg.Keystroke())
		}
	}
}

func TestPromptTUIExitQuitsWithoutExecutingCommand(t *testing.T) {
	called := false
	r := rootWithPromptCommand("exit", func(*cobra.Command, []string) error {
		called = true
		return nil
	})
	m := newIntegratedPromptModel(r, nil)
	m.editor.SetValue("exit")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	if cmd == nil {
		t.Fatal("exit did not return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("exit command did not return tea.QuitMsg")
	}
	if called || m.running {
		t.Fatalf("exit executed Cobra command: called=%v running=%v", called, m.running)
	}
}

func TestPromptTUIReceivesRendererEventsThroughTeaCommand(t *testing.T) {
	events := make(chan renderer.Event, promptRendererEventBufferSize)
	m := newPromptModel(promptModelOptions{Events: events})
	events <- renderer.Event{Kind: renderer.EventLine, Text: "from renderer"}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("model init did not return an event command")
	}
	updated, _ := m.Update(cmd())
	m = updated.(*promptModel)
	if !reflect.DeepEqual(m.transcript, []string{"from renderer"}) {
		t.Fatalf("transcript = %q", m.transcript)
	}
}

func TestPromptRendererWaitStopsWithPromptLifetime(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	result := make(chan tea.Msg, 1)
	go func() {
		result <- waitForRendererEvent(lifetime, make(chan renderer.Event))()
	}()

	cancel()
	select {
	case msg := <-result:
		if _, ok := msg.(promptRendererEventsClosedMsg); !ok {
			t.Fatalf("message type = %T, want promptRendererEventsClosedMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("renderer wait remained blocked after prompt lifetime ended")
	}
}

func TestPromptContextWaitStopsWithPromptLifetime(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	result := make(chan tea.Msg, 1)
	go func() {
		result <- waitForPromptContext(lifetime, parent)()
	}()

	cancelLifetime()
	select {
	case msg := <-result:
		if _, ok := msg.(promptContextDoneMsg); !ok {
			t.Fatalf("message type = %T, want promptContextDoneMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("parent context wait remained blocked after prompt lifetime ended")
	}
}

func TestRunPromptTUICancelsLifetimeAfterProgramReturns(t *testing.T) {
	var lifetime context.Context
	r := &Root{
		promptProgramRunner: func(model *promptModel) error {
			lifetime = model.lifetime
			return nil
		},
	}

	if err := r.runPromptTUI(context.Background(), nil, "tester"); err != nil {
		t.Fatalf("run prompt: %v", err)
	}
	if lifetime == nil {
		t.Fatal("program did not receive prompt lifetime")
	}
	select {
	case <-lifetime.Done():
	default:
		t.Fatal("prompt lifetime was not canceled after Program.Run returned")
	}
}

func TestRunPromptTUICancelsAndJoinsActiveCommandOnProgramError(t *testing.T) {
	programErr := errors.New("forced program error")
	started := make(chan struct{})
	commandReturned := make(chan struct{})
	modelReady := make(chan *promptModel, 1)
	r := rootWithPromptCommand("wait", func(cmd *cobra.Command, _ []string) error {
		close(started)
		<-cmd.Context().Done()
		sink := renderer.EventSinkFromContext(cmd.Context())
		for range promptRendererEventBufferSize + 16 {
			sink.Emit(renderer.Event{Kind: renderer.EventLine, Text: "cleanup"})
		}
		return cmd.Context().Err()
	})
	r.promptProgramRunner = func(model *promptModel) error {
		model.editor.SetValue("wait")
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(*promptModel)
		if command == nil {
			return errors.New("active command was not created")
		}
		modelReady <- model
		go func() {
			_ = command()
			close(commandReturned)
		}()
		select {
		case <-started:
			return programErr
		case <-time.After(time.Second):
			return errors.New("active command did not start")
		}
	}

	result := make(chan error, 1)
	go func() {
		result <- r.runPromptTUI(context.Background(), nil, "tester")
	}()
	var active *promptModel
	select {
	case active = <-modelReady:
	case <-time.After(time.Second):
		t.Fatal("prompt program did not create its model")
	}

	select {
	case err := <-result:
		if !errors.Is(err, programErr) {
			t.Fatalf("run prompt error = %v, want %v", err, programErr)
		}
		select {
		case <-commandReturned:
		case <-time.After(100 * time.Millisecond):
			active.cancel()
			select {
			case <-commandReturned:
			case <-time.After(time.Second):
				t.Fatal("active command did not stop during test cleanup")
			}
			t.Fatal("prompt returned before the active command")
		}
	case <-time.After(time.Second):
		active.cancel()
		deadline := time.After(time.Second)
		for {
			select {
			case <-commandReturned:
				t.Fatal("prompt deadlocked while joining the active command")
			case <-active.events:
			case <-deadline:
				t.Fatal("active command remained blocked during test cleanup")
			}
		}
	}
}

func TestPromptTUIStoresAcceptedHistoryAndFiltersSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	store, err := newPromptHistoryStore(path, 10, (&Root{}).shouldSkipPromptHistoryEntry)
	if err != nil {
		t.Fatalf("create history: %v", err)
	}
	r := &Root{promptRootFactory: func() *cobra.Command {
		root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
		root.AddCommand(&cobra.Command{Use: "capture", Args: cobra.ArbitraryArgs, RunE: func(*cobra.Command, []string) error { return nil }})
		return root
	}}
	m := newIntegratedPromptModel(r, store)

	for _, line := range []string{`capture "two words"`, `capture --token secret`} {
		m.editor.SetValue(line)
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*promptModel)
		m, _ = updatePromptCommandAndDrain(t, m, cmd())
	}

	if got := store.Entries(); !reflect.DeepEqual(got, []string{`capture "two words"`}) {
		t.Fatalf("stored history = %q", got)
	}
	if got := m.history; !reflect.DeepEqual(got, []string{`capture "two words"`}) {
		t.Fatalf("model history = %q", got)
	}
}

func TestPromptTUIEditorHistoryMatchesPersistedMaximum(t *testing.T) {
	store, err := newPromptHistoryStore(filepath.Join(t.TempDir(), "history"), 2, nil)
	if err != nil {
		t.Fatalf("create history: %v", err)
	}
	r := rootWithPromptCommand("capture", func(*cobra.Command, []string) error { return nil })
	m := newIntegratedPromptModel(r, store)

	for _, line := range []string{"capture one", "capture two", "capture three"} {
		m.editor.SetValue(line)
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*promptModel)
		m, _ = updatePromptCommandAndDrain(t, m, cmd())
	}

	want := []string{"capture two", "capture three"}
	if got := store.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted history = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(m.history, want) {
		t.Fatalf("editor history = %q, want %q", m.history, want)
	}
}

func TestPromptLogRouterSuppressesPreexistingChildLogger(t *testing.T) {
	var configuredOutput bytes.Buffer
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(&configuredOutput), zapcore.DebugLevel)
	router := newPromptLogRouter()
	logger := zap.New(newPromptRoutingCore(base, router, encoder))
	child := logger.Named("telegram").With(zap.String("scope", "client"))

	child.Info("normal output")
	if got := configuredOutput.String(); !strings.Contains(got, "normal output") {
		t.Fatalf("normal output missing from configured sink: %q", got)
	}
	configuredOutput.Reset()

	events := make(chan renderer.Event, 1)
	router.SetSink(renderer.NewChannelEventSink(events))
	child.Error("prompt output")
	if got := configuredOutput.String(); got != "" {
		t.Fatalf("prompt log escaped to configured terminal sink: %q", got)
	}
	select {
	case event := <-events:
		t.Fatalf("prompt log leaked into transcript: %+v", event)
	default:
	}

	router.SetSink(nil)
	child.Info("normal restored")
	if got := configuredOutput.String(); !strings.Contains(got, "normal restored") {
		t.Fatalf("normal output was not restored: %q", got)
	}
}

type promptMemorySink struct {
	bytes.Buffer
}

func (s *promptMemorySink) Sync() error  { return nil }
func (s *promptMemorySink) Close() error { return nil }

func TestPromptLoggerPreservesFileSinkWhileSuppressingTerminalSink(t *testing.T) {
	retainedSink := &promptMemorySink{}
	if err := zap.RegisterSink("prompt-file-test", func(*url.URL) (zap.Sink, error) {
		return retainedSink, nil
	}); err != nil {
		t.Fatalf("register retained log sink: %v", err)
	}
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{"prompt-file-test://audit", "stderr"}
	config.ErrorOutputPaths = []string{"prompt-file-test://audit"}
	router := newPromptLogRouter()
	logger, err := buildPromptLogger(config, router)
	if err != nil {
		t.Fatalf("build prompt logger: %v", err)
	}

	events := make(chan renderer.Event, 1)
	router.SetSink(renderer.NewChannelEventSink(events))
	logger.Info("file survives prompt")
	if err := logger.Sync(); err != nil {
		t.Fatalf("sync logger: %v", err)
	}

	if contents := retainedSink.String(); !strings.Contains(contents, "file survives prompt") {
		t.Fatalf("file sink lost prompt log: %q", contents)
	}
	select {
	case event := <-events:
		t.Fatalf("terminal log leaked into prompt transcript: %+v", event)
	default:
	}
}

func TestPromptLogRouterDoesNotUseZapErrorOutput(t *testing.T) {
	type recursiveValue struct {
		Self *recursiveValue
	}
	value := &recursiveValue{}
	value.Self = value

	var configuredOutput bytes.Buffer
	var zapErrorOutput bytes.Buffer
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(&configuredOutput), zapcore.DebugLevel)
	router := newPromptLogRouter()
	logger := zap.New(
		newPromptRoutingCore(base, router, encoder),
		zap.ErrorOutput(zapcore.AddSync(&zapErrorOutput)),
	)
	events := make(chan renderer.Event, 1)
	router.SetSink(renderer.NewChannelEventSink(events))

	logger.Error("unencodable prompt log", zap.Reflect("value", value))
	if got := configuredOutput.String(); got != "" {
		t.Fatalf("prompt log escaped to configured output: %q", got)
	}
	if got := zapErrorOutput.String(); got != "" {
		t.Fatalf("prompt log escaped through zap error output: %q", got)
	}
	select {
	case event := <-events:
		t.Fatalf("suppressed prompt log produced an event: %+v", event)
	default:
	}
}

func TestRunPromptTUISuppressesTerminalLogsOnlyWhileProgramRuns(t *testing.T) {
	var configuredOutput bytes.Buffer
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(&configuredOutput), zapcore.DebugLevel)
	router := newPromptLogRouter()
	logger := zap.New(newPromptRoutingCore(base, router, encoder))
	child := logger.Named("telegram")
	r := &Root{promptLogs: router}
	r.promptProgramRunner = func(model *promptModel) error {
		child.Error("inside prompt")
		select {
		case event := <-model.events:
			t.Fatalf("prompt log leaked into model events: %+v", event)
		default:
		}
		return nil
	}

	if err := r.runPromptTUI(context.Background(), nil, "tester"); err != nil {
		t.Fatalf("run prompt: %v", err)
	}
	if got := configuredOutput.String(); got != "" {
		t.Fatalf("prompt log escaped to configured output: %q", got)
	}

	child.Info("after prompt")
	if got := configuredOutput.String(); !strings.Contains(got, "after prompt") {
		t.Fatalf("configured output was not restored: %q", got)
	}
}

func TestPromptTerminalLogSuppressionNeverBlocksChildLogger(t *testing.T) {
	var configuredOutput bytes.Buffer
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(&configuredOutput), zapcore.DebugLevel)
	router := newPromptLogRouter()
	logger := zap.New(newPromptRoutingCore(base, router, encoder))
	child := logger.Named("telegram")

	events := make(chan renderer.Event, 1)
	events <- renderer.Event{Kind: renderer.EventLine, Text: "fill"}
	router.SetSink(renderer.NewChannelEventSink(events))
	writeDone := make(chan struct{})
	go func() {
		child.Error("blocked prompt log")
		close(writeDone)
	}()

	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("suppressed child logger write blocked on the prompt channel")
	}
	if got := configuredOutput.String(); got != "" {
		t.Fatalf("prompt log escaped to configured output: %q", got)
	}

	router.SetSink(nil)
	child.Info("normal output")
	if got := configuredOutput.String(); !strings.Contains(got, "normal output") {
		t.Fatalf("normal logging was not restored: %q", got)
	}
}

func TestPromptTUICommandTreeUsesProductionCommandsWithoutNestedPrompt(t *testing.T) {
	r := &Root{version: "test"}
	root := r.newPromptRootCmd()

	for _, command := range []string{"dialog", "download", "peer", "version", "exit"} {
		if found, _, err := root.Find([]string{command}); err != nil || found.Name() != command {
			t.Fatalf("command %q is unavailable: found=%v err=%v", command, found, err)
		}
	}
	if found, _, err := root.Find([]string{"prompt"}); err == nil && found.Name() == "prompt" {
		t.Fatal("interactive command tree must not contain nested prompt command")
	}
}

func TestPromptTUICommandTreePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := &Root{version: "test", level: zap.NewAtomicLevel()}
	root := r.newPromptRootCmd()
	root.SetContext(ctx)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if !errors.Is(root.Context().Err(), context.Canceled) {
		t.Fatalf("command context error = %v, want context.Canceled", root.Context().Err())
	}
}

func newIntegratedPromptModel(r *Root, history *promptHistoryStore) *promptModel {
	events := make(chan renderer.Event, promptRendererEventBufferSize)
	return r.newPromptTUIModel(context.Background(), context.Background(), history, "tester", events, renderer.NewChannelEventSink(events))
}

func updatePromptCommandAndDrain(t *testing.T, m *promptModel, msg tea.Msg) (*promptModel, tea.Cmd) {
	t.Helper()
	updated, finalCmd := m.Update(msg)
	m = updated.(*promptModel)
	for m.running {
		wait := waitForRendererEvent(m.lifetime, m.events)
		if wait == nil {
			t.Fatal("command is waiting for a barrier without an event stream")
		}
		updated, cmd := m.Update(wait())
		m = updated.(*promptModel)
		if cmd != nil {
			finalCmd = cmd
		}
	}
	return m, finalCmd
}
