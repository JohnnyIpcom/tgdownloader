package cmd

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

const promptRendererEventBufferSize = 256

func (r *Root) runPromptTUI(ctx context.Context, history *promptHistoryStore, username string) error {
	lifetime, cancelLifetime := context.WithCancel(context.Background())

	events := make(chan renderer.Event, promptRendererEventBufferSize)
	sink := renderer.NewContextChannelEventSink(lifetime, events)
	if r.promptLogs != nil {
		r.promptLogs.SetSink(sink)
	}
	defer func() {
		cancelLifetime()
		if r.promptLogs != nil {
			r.promptLogs.SetSink(nil)
		}
	}()
	model := r.newPromptTUIModel(ctx, lifetime, history, username, events, sink)
	err := r.runPromptProgram(model)
	model.cancelAndWaitForActiveCommand()
	return err
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
