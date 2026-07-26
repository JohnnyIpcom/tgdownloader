package cmd

import (
	"context"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type promptRuntimeStartupResult struct {
	Username string
	History  *promptHistoryStore
	Err      error
}

func (r *Root) startPromptRuntime(
	ctx context.Context,
	sink renderer.EventSink,
	provider telegram.CodeProvider,
) promptRuntimeStartupResult {
	if r.promptStartupRunner != nil {
		return r.promptStartupRunner(ctx, sink, provider)
	}

	progress := renderer.NewTUIProgress(sink)
	output := renderer.NewEventWriter(sink)

	runtimeTracker := progress.UnitsTracker("Runtime setup", 0)
	if err := r.initializeRuntime(
		withRuntimeProgress(progress),
		withRuntimeOutput(output),
		withRuntimeEventSink(sink),
		withTelegramClientOptions(telegram.WithCodeProvider(provider)),
	); err != nil {
		runtimeTracker.Fail()
		progress.Wait(ctx)

		return promptRuntimeStartupResult{Err: err}
	}

	if r.promptLogs != nil {
		r.promptLogs.SetSink(sink)
	}

	runtimeTracker.Done()
	progress.Wait(ctx)

	if err := r.Connect(ctx); err != nil {
		return promptRuntimeStartupResult{Err: err}
	}

	setupTracker := progress.UnitsTracker("Prompt setup", 0)

	self, err := r.client.UserService.GetSelf(ctx)
	if err != nil {
		setupTracker.Fail()
		progress.Wait(ctx)

		return promptRuntimeStartupResult{Err: err}
	}

	enabled, path, maxEntries := r.promptHistorySettings()
	var history *promptHistoryStore
	if enabled {
		history, err = newPromptHistoryStore(path, maxEntries, r.shouldSkipPromptHistoryEntry)
		if err != nil {
			writer := renderer.NewEventWriter(sink)
			renderer.RenderError(writer, err)
			writer.Flush()
			history = nil
		}
	}

	setupTracker.Done()
	progress.Wait(ctx)

	return promptRuntimeStartupResult{
		Username: self.Raw().Username,
		History:  history,
	}
}
