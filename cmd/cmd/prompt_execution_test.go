package cmd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
)

func TestSubmitPromptCommandScopesRendererEventsAndOutput(t *testing.T) {
	events := make(chan renderer.Event, 3)
	r := rootWithPromptCommand("capture", func(cmd *cobra.Command, args []string) error {
		renderer.EventSinkFromContext(cmd.Context()).Emit(renderer.Event{Kind: renderer.EventLine, Text: "scoped"})
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "written")
		return err
	})

	msg := r.submitPromptCommand(context.Background(), "capture", renderer.NewChannelEventSink(events))()
	if done := msg.(promptCommandDoneMsg); done.Err != nil {
		t.Fatalf("message = %+v", done)
	}
	first, second, barrier := <-events, <-events, <-events
	if first.Text != "scoped" || second.Text != "written" {
		t.Fatalf("events = %+v, %+v", first, second)
	}
	if barrier.Kind != renderer.EventBarrier || barrier.ID != msg.(promptCommandDoneMsg).RunID {
		t.Fatalf("barrier = %+v, done = %+v", barrier, msg)
	}
}

func TestSubmitPromptCommandFlushesFinalOutputBeforeBarrier(t *testing.T) {
	events := make(chan renderer.Event, 2)
	r := rootWithPromptCommand("capture", func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), "final fragment")
		return err
	})

	msg := r.submitPromptCommand(context.Background(), "capture", renderer.NewChannelEventSink(events))()
	done := msg.(promptCommandDoneMsg)
	if done.Err != nil {
		t.Fatalf("message = %+v", done)
	}
	received := make([]renderer.Event, 0, 2)
	for len(received) < 2 {
		select {
		case event := <-events:
			received = append(received, event)
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("events = %+v, want final fragment followed by barrier", received)
		}
	}
	line, barrier := received[0], received[1]
	if line.Kind != renderer.EventLine || line.Text != "final fragment" {
		t.Fatalf("line event = %+v", line)
	}
	if barrier.Kind != renderer.EventBarrier || barrier.ID != done.RunID {
		t.Fatalf("barrier = %+v, done = %+v", barrier, done)
	}
}

func TestSubmitPromptCommandRunsQuotedCobraArgument(t *testing.T) {
	var got []string
	r := rootWithPromptCommand("capture", func(cmd *cobra.Command, args []string) error {
		got = append([]string(nil), args...)
		return nil
	})

	msg := r.submitPromptCommand(context.Background(), `capture "two words"`, renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if done.Err != nil || !reflect.DeepEqual(got, []string{"two words"}) {
		t.Fatalf("done=%+v args=%q", done, got)
	}
}

func TestSubmitPromptCommandPreservesWindowsPathBackslashes(t *testing.T) {
	const path = `C:\Users\evgen\Downloads`
	var got []string
	r := rootWithPromptCommand("capture", func(cmd *cobra.Command, args []string) error {
		got = append([]string(nil), args...)
		return nil
	})

	msg := r.submitPromptCommand(context.Background(), "capture "+path, renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if done.Err != nil || !reflect.DeepEqual(got, []string{path}) {
		t.Fatalf("done=%+v args=%q", done, got)
	}
}

func TestSubmitPromptCommandHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	r := rootWithPromptCommand("wait", func(cmd *cobra.Command, args []string) error {
		called = true
		return nil
	})

	msg := r.submitPromptCommand(ctx, "wait", renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if !errors.Is(done.Err, context.Canceled) {
		t.Fatalf("message = %+v", done)
	}
	if called {
		t.Fatal("canceled command was invoked")
	}
}

func TestSubmitPromptCommandBuildsFreshCommandTree(t *testing.T) {
	var factoryCalls int
	r := &Root{
		promptRootFactory: func() *cobra.Command {
			factoryCalls++
			root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
			root.AddCommand(&cobra.Command{Use: "noop", RunE: func(*cobra.Command, []string) error { return nil }})
			return root
		},
	}

	for i := 0; i < 2; i++ {
		msg := r.submitPromptCommand(context.Background(), "noop", renderer.DiscardEvents())()
		if done := msg.(promptCommandDoneMsg); done.Err != nil {
			t.Fatalf("run %d: %+v", i+1, done)
		}
	}
	if factoryCalls != 2 {
		t.Fatalf("command factory calls = %d, want 2", factoryCalls)
	}
}

func TestSubmitPromptCommandConvertsPanicsToErrors(t *testing.T) {
	r := rootWithPromptCommand("panic", func(cmd *cobra.Command, args []string) error {
		panic("test panic")
	})

	msg := r.submitPromptCommand(context.Background(), "panic", renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if done.Err == nil || !strings.Contains(done.Err.Error(), "test panic") {
		t.Fatalf("message = %+v", done)
	}
}

func TestSubmitPromptCommandConvertsFactoryPanicsToErrors(t *testing.T) {
	r := &Root{
		promptRootFactory: func() *cobra.Command {
			panic("factory panic")
		},
	}

	msg := r.submitPromptCommand(context.Background(), "noop", renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if done.Err == nil || !strings.Contains(done.Err.Error(), "factory panic") {
		t.Fatalf("message = %+v", done)
	}
}

func TestSubmitPromptCommandNeverOverlapsCommands(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	r := rootWithPromptCommand("block", func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "first":
			close(firstStarted)
		case "second":
			close(secondStarted)
		}
		select {
		case <-release:
			return nil
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		}
	})

	firstDone := make(chan tea.Msg, 1)
	go func() {
		firstDone <- r.submitPromptCommand(context.Background(), "block first", renderer.DiscardEvents())()
	}()
	<-firstStarted

	secondDone := make(chan tea.Msg, 1)
	go func() {
		secondDone <- r.submitPromptCommand(context.Background(), "block second", renderer.DiscardEvents())()
	}()

	select {
	case <-secondStarted:
		t.Fatal("commands overlapped")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	assertPromptCommandDone(t, <-firstDone)
	assertPromptCommandDone(t, <-secondDone)
}

func TestSubmitPromptCommandReturnsCanceledWhileQueued(t *testing.T) {
	var factoryCalls int
	r := &Root{
		promptRootFactory: func() *cobra.Command {
			factoryCalls++
			return &cobra.Command{Use: "test"}
		},
	}
	gate := r.promptCommandGate()
	<-gate
	defer func() { gate <- struct{}{} }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := r.submitPromptCommand(ctx, "noop", renderer.DiscardEvents())()
	done := msg.(promptCommandDoneMsg)
	if !errors.Is(done.Err, context.Canceled) {
		t.Fatalf("message = %+v", done)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls = %d, want 0", factoryCalls)
	}
}

func assertPromptCommandDone(t *testing.T, msg tea.Msg) {
	t.Helper()
	done, ok := msg.(promptCommandDoneMsg)
	if !ok {
		t.Fatalf("message type = %T, want promptCommandDoneMsg", msg)
	}
	if done.Err != nil {
		t.Fatalf("message = %+v", done)
	}
}

func rootWithPromptCommand(name string, run func(*cobra.Command, []string) error) *Root {
	return &Root{
		promptRootFactory: func() *cobra.Command {
			root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
			root.AddCommand(&cobra.Command{Use: name, Args: cobra.ArbitraryArgs, RunE: run})
			return root
		},
	}
}
