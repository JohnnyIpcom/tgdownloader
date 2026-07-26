package cmd

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/go-andiamo/splitter"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func (r *Root) submitPromptCommand(
	parent context.Context,
	line string,
	events renderer.EventSink,
	histories ...*promptHistoryStore,
) tea.Cmd {
	var history *promptHistoryStore
	if len(histories) > 0 {
		history = histories[0]
	}

	return r.submitCommand(parent, line, nil, false, events, history)
}

func (r *Root) submitRuntimeCommand(parent context.Context, args []string, events renderer.EventSink) tea.Cmd {
	return r.submitCommand(parent, strings.Join(args, " "), args, true, events, nil)
}

func (r *Root) submitCommand(
	parent context.Context,
	line string,
	providedArgs []string,
	hasProvidedArgs bool,
	events renderer.EventSink,
	history *promptHistoryStore,
) tea.Cmd {
	if events == nil {
		events = renderer.DiscardEvents()
	}

	return func() (msg tea.Msg) {
		var stdoutWriter, stderrWriter *renderer.EventWriter

		done := promptCommandDoneMsg{
			RunID: strconv.FormatUint(r.promptRunID.Add(1), 10),
			Line:  line,
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				done.Err = r.promptPanicError(recovered)
			}

			if stdoutWriter != nil {
				stdoutWriter.Flush()
			}
			if stderrWriter != nil {
				stderrWriter.Flush()
			}

			r.persistPromptHistory(&done, history, events)
			events.Emit(renderer.Event{Kind: renderer.EventBarrier, ID: done.RunID})
			msg = done
		}()

		if parent == nil {
			parent = context.Background()
		}

		if err := r.acquirePromptCommand(parent); err != nil {
			done.Err = err
			return
		}
		defer r.releasePromptCommand()

		args := append([]string(nil), providedArgs...)
		if !hasProvidedArgs {
			var err error
			args, err = splitPromptLine(line)
			if err != nil {
				done.Err = err
				return
			}
		}
		done.Args = append([]string(nil), args...)

		root := r.newPromptRootCmd()
		resolved, _, findErr := root.Find(args)
		if findErr == nil && resolved != nil {
			done.HistoryOK = true
			if mode, ok := resolved.Annotations["prompt_history"]; ok && strings.EqualFold(mode, "off") {
				done.HistoryOK = false
			}
		}

		parent = renderer.WithEventSink(parent, events)
		stdoutWriter = renderer.NewEventWriter(events)
		stderrWriter = renderer.NewEventWriter(events)
		root.SetOut(stdoutWriter)
		root.SetErr(stderrWriter)
		root.SetArgs(args)
		done.Err = r.executePromptRoot(parent, root)
		return
	}
}

func (r *Root) persistPromptHistory(done *promptCommandDoneMsg, history *promptHistoryStore, events renderer.EventSink) {
	if history == nil || !done.HistoryOK {
		return
	}

	stored, err := history.Record(done.Line, done.Args)
	if err != nil {
		events.Emit(renderer.Event{Kind: renderer.EventLine, Text: fmt.Sprintf("Error: %v", err)})
		return
	}

	done.HistoryStored = stored
}

func splitPromptLine(line string) ([]string, error) {
	s := splitter.MustCreateSplitter(' ', splitter.DoubleQuotesDoubleEscaped)
	s.AddDefaultOptions(splitter.Trim(`/"`), splitter.UnescapeQuotes)
	return s.Split(line)
}

func (r *Root) executePromptRoot(parent context.Context, root *cobra.Command) (err error) {
	if err := parent.Err(); err != nil {
		return err
	}

	root.SetContext(parent)
	return root.ExecuteContext(parent)
}

func (r *Root) acquirePromptCommand(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.promptCommandGate():
		return nil
	}
}

func (r *Root) releasePromptCommand() {
	r.promptCommandGate() <- struct{}{}
}

func (r *Root) promptPanicError(recovered any) error {
	stack := debug.Stack()
	if r.zap != nil {
		r.zap.Debug("prompt command panicked", zap.Any("panic", recovered), zap.ByteString("stack", stack))
	}

	return fmt.Errorf("prompt command panicked: %v", recovered)
}
