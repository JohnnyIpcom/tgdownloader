package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
)

func TestRootRoutesRuntimeCommandToOneShotTUI(t *testing.T) {
	r := &Root{}

	var requests []runtimeCommandRequest
	r.runtimeCommandRunner = func(_ context.Context, request runtimeCommandRequest) error {
		requests = append(requests, request)
		return nil
	}

	root := r.newRootCmd()
	root.SetArgs([]string{"dialog", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute dialog list: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(requests))
	}
	if got, want := requests[0].Args, []string{"dialog", "list"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime args = %q, want %q", got, want)
	}
	if requests[0].Mode != runtimeModeOnly {
		t.Fatalf("runtime mode = %q, want %q", requests[0].Mode, runtimeModeOnly)
	}
	if r.runtimeInitialized || r.client != nil {
		t.Fatal("outer Cobra initialized runtime")
	}
}

func TestRootRoutesRuntimeFlagsAndArguments(t *testing.T) {
	r := &Root{}

	var request runtimeCommandRequest
	r.runtimeCommandRunner = func(_ context.Context, got runtimeCommandRequest) error {
		request = got
		return nil
	}

	root := r.newRootCmd()
	root.SetArgs([]string{"download", "history", "Фотограф внутреннего танца", "--limit", "25"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute download history: %v", err)
	}

	want := []string{"download", "history", "--limit=25", "Фотограф внутреннего танца"}
	if !reflect.DeepEqual(request.Args, want) {
		t.Fatalf("runtime args = %q, want %q", request.Args, want)
	}
	if request.Mode != runtimeModeRequiresConnection {
		t.Fatalf("runtime mode = %q, want %q", request.Mode, runtimeModeRequiresConnection)
	}
}

func TestRootPlainCommandsBypassOneShotTUI(t *testing.T) {
	r := &Root{version: "test"}
	r.runtimeCommandRunner = func(context.Context, runtimeCommandRequest) error {
		t.Fatal("plain command launched one-shot TUI")
		return nil
	}

	for _, args := range [][]string{{"version"}, {"--help"}, {"dialog", "list", "--help"}} {
		root := r.newRootCmd()
		root.SetArgs(args)

		if err := root.Execute(); err != nil {
			t.Fatalf("execute %q: %v", args, err)
		}
	}
}

func TestRootValidationFailureBypassesOneShotTUI(t *testing.T) {
	r := &Root{}
	r.runtimeCommandRunner = func(context.Context, runtimeCommandRequest) error {
		t.Fatal("invalid command launched one-shot TUI")
		return nil
	}

	root := r.newRootCmd()
	root.SetArgs([]string{"download", "history"})

	if err := root.Execute(); err == nil {
		t.Fatal("missing peer argument was accepted")
	}
}

func TestRuntimeCommandStartsInsideOneShotProgram(t *testing.T) {
	programEntered := false
	startupCalled := false
	commandCalled := false

	r := rootWithPromptCommand("capture", func(*cobra.Command, []string) error {
		commandCalled = true
		return nil
	})
	r.oneShotStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider, string) error {
		if !programEntered {
			t.Fatal("runtime startup ran before one-shot program")
		}

		startupCalled = true
		return nil
	}
	r.oneShotProgramRunner = func(model *oneShotModel) error {
		programEntered = true

		updated, command := model.Update(model.startup())
		model = updated.(*oneShotModel)
		if command == nil {
			t.Fatal("startup did not schedule runtime command")
		}

		updated, quit := model.Update(runOneShotExecutionCommand(t, command))
		model = updated.(*oneShotModel)
		if quit != nil {
			t.Fatal("command quit before barrier")
		}

		for quit == nil {
			event := <-model.events
			updated, quit = model.Update(promptRendererEventMsg{event: event})
			model = updated.(*oneShotModel)
		}

		return nil
	}

	err := r.runOneShotTUI(context.Background(), runtimeCommandRequest{
		Mode: runtimeModeOnly,
		Args: []string{"capture"},
	})
	if err != nil {
		t.Fatalf("run one-shot: %v", err)
	}
	if !startupCalled || !commandCalled {
		t.Fatalf("startup=%v command=%v", startupCalled, commandCalled)
	}
}

func TestRuntimeCommandReturnsOriginalStartupError(t *testing.T) {
	startupErr := errors.New("forced startup failure")
	r := &Root{}
	r.oneShotStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider, string) error {
		return startupErr
	}
	r.oneShotProgramRunner = func(model *oneShotModel) error {
		updated, command := model.Update(model.startup())
		model = updated.(*oneShotModel)
		if command != nil || model.state != promptStateFailed {
			t.Fatalf("startup failure state=%v command=%v", model.state, command != nil)
		}

		updated, quit := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		model = updated.(*oneShotModel)
		if quit == nil {
			t.Fatal("failed startup did not wait for Ctrl+C")
		}

		return nil
	}

	err := r.runOneShotTUI(context.Background(), runtimeCommandRequest{Mode: runtimeModeOnly})
	if !errors.Is(err, startupErr) {
		t.Fatalf("run one-shot error = %v, want startup error", err)
	}
}

func TestPresentedRuntimeErrorIsNotRenderedTwice(t *testing.T) {
	original := errors.New("already shown")
	err := &presentedRuntimeError{err: original}
	var output bytes.Buffer

	renderUnpresentedError(&output, err)

	if output.Len() != 0 {
		t.Fatalf("duplicate output = %q", output.String())
	}
	if !errors.Is(err, original) {
		t.Fatalf("presented error does not unwrap: %v", err)
	}
}
