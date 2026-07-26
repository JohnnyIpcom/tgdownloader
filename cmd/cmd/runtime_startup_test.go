package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

func TestRuntimeStartupBeginsAfterPromptProgramStarts(t *testing.T) {
	startupCalled := false
	programEntered := false

	r := &Root{version: "test"}
	r.promptStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider) promptRuntimeStartupResult {
		if !programEntered {
			t.Fatal("runtime startup ran before prompt program")
		}
		startupCalled = true
		return promptRuntimeStartupResult{Username: "tester"}
	}

	r.promptProgramRunner = func(model *promptModel) error {
		programEntered = true
		if startupCalled || r.runtimeInitialized {
			t.Fatal("runtime initialized before Bubble Tea runner")
		}
		updated, _ := model.Update(model.startup())
		model = updated.(*promptModel)

		if model.state != promptStateReady || model.username != "tester" {
			t.Fatalf("startup result state=%v username=%q", model.state, model.username)
		}
		return nil
	}

	if err := r.runPromptTUI(context.Background()); err != nil {
		t.Fatalf("runPromptTUI() error = %v", err)
	}

	if !startupCalled {
		t.Fatal("runtime startup was not called")
	}
}

func TestRuntimeStartupFailureRemainsVisibleAndReturnsOriginalError(t *testing.T) {
	startupErr := errors.New("forced startup failure")

	r := &Root{version: "test"}
	r.promptStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider) promptRuntimeStartupResult {
		return promptRuntimeStartupResult{Err: startupErr}
	}

	r.promptProgramRunner = func(model *promptModel) error {
		updated, _ := model.Update(model.startup())
		model = updated.(*promptModel)

		if model.state != promptStateFailed || !strings.Contains(model.render(), "forced startup failure") {
			t.Fatalf("startup failure not retained: state=%v view=%q", model.state, model.render())
		}
		updated, quit := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		model = updated.(*promptModel)

		if quit == nil {
			t.Fatal("failed state did not wait for Ctrl+C")
		}
		return nil
	}

	err := r.runPromptTUI(context.Background())
	if !errors.Is(err, startupErr) {
		t.Fatalf("runPromptTUI() error = %v, want original startup error", err)
	}
}

func TestRuntimeStartupIsCanceledAndJoinedWhenProgramStops(t *testing.T) {
	programErr := errors.New("program stopped")
	started := make(chan struct{})
	finished := make(chan struct{})

	r := &Root{version: "test"}
	r.promptStartupRunner = func(ctx context.Context, _ renderer.EventSink, _ telegram.CodeProvider) promptRuntimeStartupResult {
		close(started)
		<-ctx.Done()
		close(finished)
		return promptRuntimeStartupResult{Err: ctx.Err()}
	}

	r.promptProgramRunner = func(model *promptModel) error {
		go model.startup()
		select {
		case <-started:
			return programErr
		case <-time.After(time.Second):
			return errors.New("startup did not begin")
		}
	}

	err := r.runPromptTUI(context.Background())
	if !errors.Is(err, programErr) {
		t.Fatalf("runPromptTUI() error = %v, want program error", err)
	}

	select {
	case <-finished:
	default:
		t.Fatal("runPromptStartupTUI returned before startup goroutine")
	}
}

func TestPromptCommandDoesNotInitializeRuntimeBeforeProgram(t *testing.T) {
	r := &Root{version: "test"}
	r.promptStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider) promptRuntimeStartupResult {
		return promptRuntimeStartupResult{Username: "tester"}
	}

	r.promptProgramRunner = func(model *promptModel) error {
		if r.runtimeInitialized {
			t.Fatal("prompt PreRun initialized runtime before Bubble Tea")
		}
		_, _ = model.Update(model.startup())
		return nil
	}
	root := r.newRootCmd()
	root.SetArgs([]string{"prompt"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute prompt: %v", err)
	}
}

func TestRuntimeStartupCannotBeginAfterProgramReturns(t *testing.T) {
	programErr := errors.New("program stopped before init")
	startupCalled := false
	var lateStartup tea.Cmd

	r := &Root{version: "test"}
	r.promptStartupRunner = func(context.Context, renderer.EventSink, telegram.CodeProvider) promptRuntimeStartupResult {
		startupCalled = true
		return promptRuntimeStartupResult{}
	}

	r.promptProgramRunner = func(model *promptModel) error {
		lateStartup = model.startup
		return programErr
	}

	if err := r.runPromptTUI(context.Background()); !errors.Is(err, programErr) {
		t.Fatalf("runPromptTUI() error = %v", err)
	}

	if lateStartup == nil {
		t.Fatal("startup command was not captured")
	}

	_ = lateStartup()
	if startupCalled {
		t.Fatal("runtime startup began after prompt program returned")
	}
}
