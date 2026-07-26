package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
)

func TestOneShotModelUsesInlineViewAndStructuredOutput(t *testing.T) {
	m := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	m.width = 80

	m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: "plain output"})
	m.applyRendererEvent(renderer.Event{
		Kind: renderer.EventTable,
		Table: &renderer.TableData{
			Columns: []renderer.TableColumn{{Header: "Name", Required: true}},
			Rows:    [][]string{{"Фотограф внутреннего танца"}},
		},
	})
	m.applyRendererEvent(renderer.Event{
		Kind:    renderer.EventProgressDone,
		ID:      "download",
		Label:   "video.mp4",
		Current: 10,
		Total:   10,
		Elapsed: time.Second,
	})

	view := m.View()
	if view.AltScreen {
		t.Fatal("one-shot view enabled alternate screen")
	}

	rendered := sanitizePromptModelText(view.Content)
	for _, want := range []string{"plain output", "Фотограф внутреннего танца", "video.mp4", "done!"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("one-shot view missing %q:\n%s", want, rendered)
		}
	}
}

func TestOneShotModelWrapsLongTextWithinTerminalWidth(t *testing.T) {
	m := newOneShotModel(oneShotModelOptions{Context: context.Background()})
	m.width = 30
	m.applyRendererEvent(renderer.Event{
		Kind: renderer.EventLine,
		Text: "Error: open a very long path containing кириллицу and more details",
	})

	lines := strings.Split(m.View().Content, "\n")
	if len(lines) < 2 {
		t.Fatalf("long output was not wrapped: %q", m.View().Content)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line width = %d, want <= %d: %q", got, m.width, line)
		}
	}
}

func TestOneShotModelRunsCommandAfterStartupAndBarrier(t *testing.T) {
	executed := 0
	m := newOneShotModel(oneShotModelOptions{
		Context: context.Background(),
		Execute: func(context.Context) tea.Cmd {
			executed++
			return func() tea.Msg { return promptCommandDoneMsg{RunID: "run-1"} }
		},
	})

	updated, command := m.Update(oneShotStartupDoneMsg{})
	m = updated.(*oneShotModel)
	if command == nil || executed != 1 {
		t.Fatalf("startup command = %v executed=%d", command != nil, executed)
	}

	updated, quit := m.Update(runOneShotExecutionCommand(t, command))
	m = updated.(*oneShotModel)
	if quit != nil {
		t.Fatal("one-shot quit before renderer barrier")
	}

	updated, quit = m.Update(promptRendererEventMsg{event: renderer.Event{Kind: renderer.EventBarrier, ID: "run-1"}})
	m = updated.(*oneShotModel)
	if quit == nil {
		t.Fatal("one-shot did not quit after command and barrier")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatalf("quit command = %T", quit())
	}
}

func TestOneShotModelShowsPreparingCommandUntilFirstRendererEvent(t *testing.T) {
	m := newOneShotModel(oneShotModelOptions{
		Context: context.Background(),
		Execute: func(context.Context) tea.Cmd {
			return func() tea.Msg { return nil }
		},
	})

	updated, command := m.Update(oneShotStartupDoneMsg{})
	m = updated.(*oneShotModel)
	if command == nil {
		t.Fatal("startup did not schedule command")
	}
	if view := m.View().Content; !strings.Contains(view, "Preparing command") {
		t.Fatalf("preparation progress missing: %q", view)
	}

	frame := m.progressFrame
	updated, tick := m.Update(promptProgressTickMsg{})
	m = updated.(*oneShotModel)
	if tick == nil || m.progressFrame != frame+1 {
		t.Fatalf("preparation progress is not animated: tick=%v frame=%d", tick != nil, m.progressFrame)
	}

	updated, _ = m.Update(promptRendererEventMsg{event: renderer.Event{Kind: renderer.EventLine, Text: "started"}})
	m = updated.(*oneShotModel)
	if view := m.View().Content; strings.Contains(view, "Preparing command") {
		t.Fatalf("preparation progress remained after command output: %q", view)
	}
}

func TestOneShotModelMasksAuthenticationCode(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()

	provider := newTUIAuthCodeProvider(lifetime)
	m := newOneShotModel(oneShotModelOptions{
		Context:      context.Background(),
		Lifetime:     lifetime,
		AuthRequests: provider.Requests(),
	})

	result := make(chan string, 1)
	go func() {
		code, _ := provider.Code(context.Background(), &tg.AuthSentCode{})
		result <- code
	}()

	request := waitForAuthCodeRequest(lifetime, provider.Requests())()
	updated, _ := m.Update(request)
	m = updated.(*oneShotModel)
	m = updateOneShotKeys(t, m, "12345")

	if view := m.View().Content; strings.Contains(view, "12345") {
		t.Fatalf("authentication code is visible: %q", view)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*oneShotModel)

	select {
	case code := <-result:
		if code != "12345" {
			t.Fatalf("provider code = %q", code)
		}
	case <-time.After(time.Second):
		t.Fatal("authentication code was not delivered")
	}
}

func TestOneShotModelReturnsCommandErrorAfterBarrier(t *testing.T) {
	commandErr := errors.New("forced command failure")
	m := newOneShotModel(oneShotModelOptions{
		Context: context.Background(),
		Execute: func(context.Context) tea.Cmd {
			return func() tea.Msg {
				return promptCommandDoneMsg{RunID: "run-1", Err: commandErr}
			}
		},
	})

	updated, command := m.Update(oneShotStartupDoneMsg{})
	m = updated.(*oneShotModel)
	updated, quit := m.Update(runOneShotExecutionCommand(t, command))
	m = updated.(*oneShotModel)

	if quit != nil {
		t.Fatal("failed command quit before barrier")
	}

	updated, quit = m.Update(promptRendererEventMsg{event: renderer.Event{Kind: renderer.EventBarrier, ID: "run-1"}})
	m = updated.(*oneShotModel)

	if quit == nil || !errors.Is(m.commandErr, commandErr) {
		t.Fatalf("quit=%v command error=%v", quit != nil, m.commandErr)
	}
	if !strings.Contains(m.View().Content, "forced command failure") {
		t.Fatalf("command error missing from view: %q", m.View().Content)
	}
}

func TestOneShotModelCancelsRunningCommand(t *testing.T) {
	commandContext := make(chan context.Context, 1)
	m := newOneShotModel(oneShotModelOptions{
		Context: context.Background(),
		Execute: func(ctx context.Context) tea.Cmd {
			commandContext <- ctx
			return func() tea.Msg { return nil }
		},
	})

	updated, _ := m.Update(oneShotStartupDoneMsg{})
	m = updated.(*oneShotModel)
	ctx := <-commandContext

	updated, quit := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*oneShotModel)

	if quit != nil || m.state != promptStateStopping {
		t.Fatalf("quit=%v state=%v", quit != nil, m.state)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("command context error = %v", ctx.Err())
	}
}

func updateOneShotKeys(t *testing.T, model *oneShotModel, keys string) *oneShotModel {
	t.Helper()

	for _, key := range keys {
		updated, _ := model.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		model = updated.(*oneShotModel)
	}

	return model
}

func runOneShotExecutionCommand(t *testing.T, command tea.Cmd) tea.Msg {
	t.Helper()

	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return message
	}

	for _, nested := range batch {
		message = nested()
		if _, tick := message.(promptProgressTickMsg); !tick {
			return message
		}
	}

	t.Fatal("one-shot batch did not contain execution command")
	return nil
}
