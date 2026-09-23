package cmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

func TestRuntimeErrorsHaveColorAcrossWrappedLines(t *testing.T) {
	for _, width := range []int{20, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			failure := errors.New("failed to resolve channel: rpc error code 400: CHANNEL_INVALID")
			prompt := newTestPromptModel(nil)
			prompt.resize(width+2, 24)
			prompt.finishCommand(promptCommandDoneMsg{Err: failure})

			oneShot := newOneShotModel(oneShotModelOptions{Context: context.Background()})
			oneShot.width = width
			oneShot.appendError(failure)

			promptLines := prompt.renderOutputBlocks(width)
			oneShotLines := strings.Split(oneShot.render(), "\n")
			if strings.Join(promptLines, "\n") != strings.Join(oneShotLines, "\n") {
				t.Fatal("prompt and one-shot error formatting differ")
			}

			for _, line := range promptLines {
				assertRuntimeLineColor(t, line, "31")
				if ansi.StringWidth(line) > width {
					t.Fatalf("line exceeds width %d: %q", width, line)
				}
			}

			if !regexp.MustCompile(`\x1b\[(?:[0-9]+;)*1(?:;[0-9]+)*m`).MatchString(promptLines[0]) {
				t.Fatalf("error prefix is not bold: %q", promptLines[0])
			}
		})
	}
}

func TestRuntimeRendererErrorKeepsColorAndPlainTextStaysPlain(t *testing.T) {
	events := make(chan renderer.Event, 8)
	writer := renderer.NewEventWriter(renderer.NewChannelEventSink(events))
	renderer.RenderErrorConcise(writer, errors.New("failure\x1b[2J"))
	fmt.Fprintln(writer, "Error: ordinary command output")
	writer.Flush()
	close(events)

	prompt := newTestPromptModel(nil)
	oneShot := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	oneShot.width = 80
	for event := range events {
		prompt.applyRendererEvent(event)
		oneShot.applyRendererEvent(event)
	}

	for _, output := range []string{strings.Join(prompt.renderOutputBlocks(80), "\n"), oneShot.render()} {
		lines := strings.Split(output, "\n")
		if len(lines) != 2 || ansi.Strip(lines[0]) != "Error: failure" || lines[1] != "Error: ordinary command output" {
			t.Fatalf("unexpected output: %q", output)
		}

		assertRuntimeLineColor(t, lines[0], "31")
		if strings.Contains(output, "\x1b[2J") {
			t.Fatal("unsafe escape sequence survived")
		}
	}
}

func TestRuntimeCancellationIsYellow(t *testing.T) {
	prompt := newTestPromptModel(nil)
	prompt.finishCommand(promptCommandDoneMsg{Err: context.Canceled})

	oneShot := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	oneShot.width = 80
	oneShot.pendingDone["test"] = promptCommandDoneMsg{Err: context.Canceled}
	oneShot.barriers["test"] = struct{}{}
	oneShot.finalizeCommand("test")

	for _, output := range []string{strings.Join(prompt.renderOutputBlocks(80), "\n"), oneShot.render()} {
		if ansi.Strip(output) != "Interrupted" {
			t.Fatalf("cancellation text = %q", output)
		}

		assertRuntimeLineColor(t, output, "33")
	}
}

func assertRuntimeLineColor(t *testing.T, line, color string) {
	t.Helper()

	pattern := `\x1b\[(?:[0-9]+;)*` + color + `(?:;[0-9]+)*m`
	if !regexp.MustCompile(pattern).MatchString(line) {
		t.Fatalf("line lacks ANSI color %s: %q", color, line)
	}
}
