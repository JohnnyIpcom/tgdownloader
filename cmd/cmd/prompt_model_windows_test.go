package cmd

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

func TestPromptModelHandlesCtrlVClipboardResult(t *testing.T) {
	const pasted = "download history Clipboard Channel"
	original, _ := clipboard.ReadAll()
	if err := clipboard.WriteAll(pasted); err != nil {
		t.Fatalf("write clipboard: %v", err)
	}
	t.Cleanup(func() {
		if err := clipboard.WriteAll(original); err != nil {
			t.Errorf("restore clipboard: %v", err)
		}
	})

	var completedLine string
	m := newPromptModel(promptModelOptions{
		Complete: func(_ context.Context, line string, _ int) completionResult {
			completedLine = line
			return completionResult{}
		},
	})
	updated, paste := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	m = updated.(*promptModel)
	if paste == nil {
		t.Fatal("ctrl+v did not request clipboard contents")
	}

	updated, _ = m.Update(paste())
	m = updated.(*promptModel)
	if got := m.editor.Value(); got != pasted {
		t.Fatalf("editor value = %q, want %q", got, pasted)
	}
	if completedLine != pasted {
		t.Fatalf("completion line = %q, want %q", completedLine, pasted)
	}
}
