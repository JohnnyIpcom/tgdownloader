package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

func TestPromptStartupDisablesEditorUntilReady(t *testing.T) {
	completionCalls := 0
	m := newPromptModel(promptModelOptions{
		Startup: func() tea.Msg { return promptStartupDoneMsg{} },
		Complete: func(context.Context, string, int) completionResult {
			completionCalls++
			return completionResult{}
		},
	})

	if m.state != promptStateStarting || m.editor.Focused() {
		t.Fatalf("initial state = %v focused=%v", m.state, m.editor.Focused())
	}

	m = updateKeys(t, m, "download history")
	if m.editor.Value() != "" || completionCalls != 0 {
		t.Fatalf("startup accepted input: value=%q completionCalls=%d", m.editor.Value(), completionCalls)
	}

	updated, _ := m.Update(promptStartupDoneMsg{Username: "tester", History: []string{"dialog list"}, HistoryLimit: 10})
	m = updated.(*promptModel)

	if m.state != promptStateReady || !m.editor.Focused() || !m.connected || m.username != "tester" {
		t.Fatalf("ready state = %v focused=%v connected=%v username=%q", m.state, m.editor.Focused(), m.connected, m.username)
	}
}

func TestPromptAuthCodeIsMaskedAndNeverStored(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()

	provider := newTUIAuthCodeProvider(lifetime)
	m := newPromptModel(promptModelOptions{
		Lifetime:     lifetime,
		Startup:      func() tea.Msg { return promptStartupDoneMsg{} },
		AuthRequests: provider.Requests(),
	})

	result := make(chan string, 1)
	go func() {
		code, _ := provider.Code(context.Background(), &tg.AuthSentCode{})
		result <- code
	}()

	requestMsg := waitForAuthCodeRequest(lifetime, provider.Requests())()
	updated, _ := m.Update(requestMsg)
	m = updated.(*promptModel)

	if m.state != promptStateAuth || m.editor.Prompt != "code>" || !m.editor.Focused() {
		t.Fatalf("auth state = %v prompt=%q focused=%v", m.state, m.editor.Prompt, m.editor.Focused())
	}
	m = updateKeys(t, m, "12345")
	if view := m.render(); strings.Contains(view, "12345") {
		t.Fatalf("authentication code is visible: %q", view)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*promptModel)
	select {
	case code := <-result:
		if code != "12345" {
			t.Fatalf("provider code = %q", code)
		}
	case <-time.After(time.Second):
		t.Fatal("authentication code was not delivered")
	}

	if m.state != promptStateStarting || m.editor.Value() != "" || len(m.transcript) != 0 || len(m.history) != 0 {
		t.Fatalf("code leaked after submit: state=%v value=%q transcript=%q history=%q", m.state, m.editor.Value(), m.transcript, m.history)
	}
}

func TestPromptStartupFailureStaysVisibleUntilCtrlC(t *testing.T) {
	startupErr := errors.New("startup secret\x1b[2J failure")
	m := newPromptModel(promptModelOptions{Startup: func() tea.Msg { return promptStartupDoneMsg{} }})

	updated, cmd := m.Update(promptStartupDoneMsg{Err: startupErr})
	m = updated.(*promptModel)

	if cmd != nil || m.state != promptStateFailed || m.editor.Focused() {
		t.Fatalf("failed state = %v focused=%v cmd=%v", m.state, m.editor.Focused(), cmd != nil)
	}

	view := m.render()
	if !strings.Contains(view, "Error: startup secret failure") || strings.Contains(view, "\x1b[2J") {
		t.Fatalf("startup error rendering = %q", view)
	}

	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*promptModel)
	if cmd == nil {
		t.Fatal("failed Ctrl+C did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("failed Ctrl+C command = %T", cmd())
	}
	if !errors.Is(m.startupErr, startupErr) {
		t.Fatalf("stored startup error = %v", m.startupErr)
	}
}

func TestPromptStartupCancellationWaitsForDoneMessage(t *testing.T) {
	canceled := false
	m := newPromptModel(promptModelOptions{
		Startup:       func() tea.Msg { return promptStartupDoneMsg{} },
		StartupCancel: func() { canceled = true },
	})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*promptModel)

	if !canceled || cmd != nil || m.state != promptStateStopping {
		t.Fatalf("cancel state = %v canceled=%v cmd=%v", m.state, canceled, cmd != nil)
	}
	updated, cmd = m.Update(promptStartupDoneMsg{Err: context.Canceled})
	m = updated.(*promptModel)

	if cmd == nil {
		t.Fatal("startup completion did not quit after cancellation")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("completion command = %T", cmd())
	}
}

func TestPromptContextCancellationQuitsFailedStartup(t *testing.T) {
	m := newPromptModel(promptModelOptions{Startup: func() tea.Msg { return promptStartupDoneMsg{} }})
	updated, _ := m.Update(promptStartupDoneMsg{Err: errors.New("failed")})
	m = updated.(*promptModel)

	updated, cmd := m.Update(promptContextDoneMsg{})
	m = updated.(*promptModel)

	if cmd == nil {
		t.Fatal("failed startup ignored parent cancellation")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("parent cancellation command = %T", cmd())
	}
}
