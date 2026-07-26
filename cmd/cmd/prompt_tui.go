package cmd

import (
	"context"
	"errors"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

const promptRendererEventBufferSize = 256

func (r *Root) runPromptTUI(ctx context.Context) error {
	lifetime, cancelLifetime := context.WithCancel(context.Background())

	events := make(chan renderer.Event, promptRendererEventBufferSize)
	sink := renderer.NewContextChannelEventSink(lifetime, events)
	if r.promptLogs != nil {
		r.promptLogs.SetSink(sink)
	}

	provider := newTUIAuthCodeProvider(lifetime)
	startupCtx, cancelStartup := context.WithCancel(ctx)
	startupDone := make(chan struct{})

	var startupClaimed atomic.Bool
	var history *promptHistoryStore

	startup := func() tea.Msg {
		if !startupClaimed.CompareAndSwap(false, true) {
			return promptStartupDoneMsg{Err: context.Canceled}
		}

		defer close(startupDone)

		result := r.startPromptRuntime(startupCtx, sink, provider)
		history = result.History

		return promptStartupDoneMsg{
			Username:     result.Username,
			History:      historyEntries(result.History),
			HistoryLimit: historyLimit(result.History),
			Err:          result.Err,
		}
	}

	model := newPromptModel(promptModelOptions{
		Context:       ctx,
		Lifetime:      lifetime,
		Version:       r.version,
		Complete:      r.completePrompt,
		Events:        events,
		Startup:       startup,
		StartupCancel: cancelStartup,
		AuthRequests:  provider.Requests(),
		Submit: func(commandCtx context.Context, line string) tea.Cmd {
			return r.submitPromptCommand(commandCtx, line, sink, history)
		},
	})

	runErr := r.runPromptProgram(model)

	cancelStartup()
	model.cancelAndWaitForActiveCommand()
	if startupClaimed.CompareAndSwap(false, true) {
		close(startupDone)
	}
	waitForPromptStartup(startupDone, events)

	cancelLifetime()
	if r.promptLogs != nil {
		r.promptLogs.SetSink(nil)
	}

	if model.startupErr != nil && !errors.Is(model.startupErr, context.Canceled) {
		return errors.Join(runErr, model.startupErr)
	}

	return runErr
}

func waitForPromptStartup(done <-chan struct{}, events <-chan renderer.Event) {
	for {
		select {
		case <-done:
			return
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		}
	}
}

func (m *promptModel) cancelAndWaitForActiveCommand() {
	if m.cancel != nil {
		m.cancel()
	}

	done := m.activeCommandDone
	if done == nil {
		return
	}

	events := m.events
	for {
		select {
		case <-done:
			return
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		}
	}
}

func (r *Root) runPromptProgram(model *promptModel) error {
	if r.promptProgramRunner != nil {
		return r.promptProgramRunner(model)
	}

	_, err := tea.NewProgram(model).Run()
	return err
}

func (r *Root) newPromptTUIModel(
	ctx context.Context,
	lifetime context.Context,
	history *promptHistoryStore,
	username string,
	events <-chan renderer.Event,
	sink renderer.EventSink,
) *promptModel {
	return newPromptModel(promptModelOptions{
		Context:   ctx,
		Lifetime:  lifetime,
		Username:  username,
		Version:   r.version,
		Connected: r.IsConnected(),
		Complete:  r.completePrompt,
		Submit: func(commandCtx context.Context, line string) tea.Cmd {
			return r.submitPromptCommand(commandCtx, line, sink, history)
		},
		Events:       events,
		History:      historyEntries(history),
		HistoryLimit: historyLimit(history),
	})
}

func historyEntries(history *promptHistoryStore) []string {
	if history == nil {
		return nil
	}

	return history.Entries()
}

func historyLimit(history *promptHistoryStore) int {
	if history == nil {
		return 0
	}

	return history.maxEntries
}
