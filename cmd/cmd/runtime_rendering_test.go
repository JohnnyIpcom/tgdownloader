package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
)

func TestPromptAndOneShotRenderSameCommandEvents(t *testing.T) {
	r := rootWithPromptCommand("render", func(cmd *cobra.Command, _ []string) error {
		sink := renderer.EventSinkFromContext(cmd.Context())
		sink.Emit(renderer.Event{Kind: renderer.EventLine, Text: "plain output"})
		sink.Emit(renderer.Event{
			Kind: renderer.EventTable,
			Table: &renderer.TableData{
				Columns: []renderer.TableColumn{{Header: "Name", Required: true}},
				Rows:    [][]string{{"Фотограф внутреннего танца"}},
			},
		})
		sink.Emit(renderer.Event{
			Kind:    renderer.EventProgressDone,
			ID:      "download",
			Label:   "video.mp4",
			Current: 10,
			Total:   10,
			Elapsed: time.Second,
		})
		return nil
	})

	promptEvents := runRenderedCommand(t, func(sink renderer.EventSink) promptCommandDoneMsg {
		return r.submitPromptCommand(context.Background(), "render", sink)().(promptCommandDoneMsg)
	})
	oneShotEvents := runRenderedCommand(t, func(sink renderer.EventSink) promptCommandDoneMsg {
		return r.submitRuntimeCommand(context.Background(), []string{"render"}, sink)().(promptCommandDoneMsg)
	})

	prompt := newTestPromptModel(nil)
	prompt.resize(80, 24)
	for _, event := range promptEvents {
		prompt.applyRendererEvent(event)
	}

	oneShot := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	oneShot.width = 80
	for _, event := range oneShotEvents {
		oneShot.applyRendererEvent(event)
	}

	promptOutput := sanitizePromptModelText(strings.Join(prompt.renderOutputBlocks(80), "\n"))
	oneShotOutput := sanitizePromptModelText(oneShot.render())
	if promptOutput != oneShotOutput {
		t.Fatalf("prompt and one-shot output differ:\nprompt:\n%s\none-shot:\n%s", promptOutput, oneShotOutput)
	}
}

func TestOneShotModelIgnoresDuplicateAndLateTrackerEvents(t *testing.T) {
	m := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressCreate, ID: "work", Label: "work", Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressDone, ID: "work", Label: "work", Current: 1, Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressDone, ID: "work", Label: "duplicate", Current: 1, Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressUpdate, ID: "work", Label: "late", Total: 1})

	output := sanitizePromptModelText(m.render())
	if strings.Count(output, "done!") != 1 {
		t.Fatalf("terminal tracker count is not stable: %q", output)
	}
	if strings.Contains(output, "duplicate") || strings.Contains(output, "late") || len(m.activeRows) != 0 {
		t.Fatalf("late tracker event changed terminal output: %q", output)
	}
}

func runRenderedCommand(
	t *testing.T,
	run func(renderer.EventSink) promptCommandDoneMsg,
) []renderer.Event {
	t.Helper()

	events := make(chan renderer.Event, 8)
	done := run(renderer.NewChannelEventSink(events))
	if done.Err != nil {
		t.Fatalf("run command: %v", done.Err)
	}

	var rendered []renderer.Event
	for {
		event := <-events
		if event.Kind == renderer.EventBarrier {
			if event.ID != done.RunID {
				t.Fatalf("barrier ID = %q, want %q", event.ID, done.RunID)
			}
			return rendered
		}

		rendered = append(rendered, event)
	}
}
