package cmd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

func TestPromptModelShowsSubstringCandidatesWhileTyping(t *testing.T) {
	m := newTestPromptModel(candidateNames("Фотограф внутреннего танца", "Фотоархив"))
	m = updateKeys(t, m, "download history фо")
	if got := len(m.completions); got != 2 {
		t.Fatalf("completion count = %d, want 2", got)
	}
}

func TestPromptModelHandlesBracketedPaste(t *testing.T) {
	const pasted = "download history Cherry Channel"
	var completedLine string
	m := newPromptModel(promptModelOptions{
		Complete: func(_ context.Context, line string, _ int) completionResult {
			completedLine = line
			return completionResult{}
		},
	})

	updated, _ := m.Update(tea.PasteMsg{Content: pasted})
	m = updated.(*promptModel)

	if got := m.editor.Value(); got != pasted {
		t.Fatalf("editor value = %q, want %q", got, pasted)
	}
	if completedLine != pasted {
		t.Fatalf("completion line = %q, want %q", completedLine, pasted)
	}
}

func TestPromptModelBlocksEditorWhileCommandRuns(t *testing.T) {
	m := newTestPromptModel(nil)
	m.editor.SetValue("version")
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.running || m.editor.Focused() {
		t.Fatalf("running=%v focused=%v", m.running, m.editor.Focused())
	}
}

func TestPromptModelKeepsSixCompletionRows(t *testing.T) {
	m := newTestPromptModel(candidateNames("1", "2", "3", "4", "5", "6", "7"))
	m = updateKeys(t, m, "d")
	if got := len(m.visibleCompletions()); got != 6 {
		t.Fatalf("visible completions = %d, want 6", got)
	}
}

func TestPromptPeerCandidateRendersEmojiAndFullID(t *testing.T) {
	peer := cachedChannel(123, "🍒🍒🍒")
	fullID := renderer.RenderTDLibPeerID(peer.TDLibPeerID())
	r := rootWithDialogPeers(peer)
	line := "download history 🍒"
	result := r.completePrompt(context.Background(), line, len([]rune(line)))
	if result.Err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result = %+v, want one candidate", result)
	}

	m := newTestPromptModel(result.Candidates)
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 80, Height: 12})
	m = updateKeys(t, m, line)
	row := m.render()
	if !strings.Contains(row, "🍒🍒🍒") || !strings.Contains(row, fullID) {
		t.Fatalf("peer identity missing from %q", row)
	}
}

func TestPromptPeerCandidateTruncatesNameByGrapheme(t *testing.T) {
	const family = "👨‍👩‍👧‍👦"
	const fullID = "0x000000000000007B"
	m := newTestPromptModel([]promptCandidate{{
		Value:       family,
		Display:     family,
		Description: "Channel | " + fullID,
	}})
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 24, Height: 12})
	m = updateKeys(t, m, "d")

	row := m.render()
	if !utf8.ValidString(row) {
		t.Fatalf("row contains split UTF-8: %q", row)
	}
	if strings.Contains(row, family) {
		t.Fatalf("row preserved an over-wide family grapheme: %q", row)
	}
	for _, partial := range []string{"👨", "👩", "👧", "👦", "\u200d"} {
		if strings.Contains(row, partial) {
			t.Fatalf("row contains partial family grapheme %q: %q", partial, row)
		}
	}
	if !strings.Contains(row, fullID) {
		t.Fatalf("row = %q, want grapheme-safe truncation and full ID", row)
	}
}

func TestPromptPeerCandidateHidesTypeBeforeNameAtExactMetadataWidth(t *testing.T) {
	const fullID = "0x000000000000007B"
	const metadataWidth = len("  ") + len("Channel | "+fullID)
	candidate := promptCandidate{
		Display:     "Cherry Channel",
		Description: "Channel | " + fullID,
	}

	got := formatPromptCandidate(candidate, metadataWidth)
	if !strings.Contains(got, "Cherry ...") || !strings.Contains(got, fullID) || strings.Contains(got, "Channel |") {
		t.Fatalf("formatted candidate = %q, want reduced name, full ID, and no type", got)
	}
}

func TestPromptCommandCandidateTruncatesDescriptionFirst(t *testing.T) {
	m := newTestPromptModel([]promptCandidate{{
		Value:       "download",
		Display:     "download",
		Description: "download media from dialog history",
	}})
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 25, Height: 12})
	m = updateKeys(t, m, "d")

	row := m.render()
	if !strings.Contains(row, "download") || !strings.Contains(row, "...") {
		t.Fatalf("row = %q, want command name and truncated description", row)
	}
	if strings.Contains(row, "download media from dialog history") {
		t.Fatalf("row did not truncate command description: %q", row)
	}
}

func TestPromptModelScrollsCompletionWindow(t *testing.T) {
	m := newTestPromptModel(candidateNames("one", "two", "three", "four", "five", "six", "seven", "eight"))
	m = updateKeys(t, m, "d")

	for range 6 {
		m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.selected != 6 || m.visibleCompletions()[0].Value != "two" {
		t.Fatalf("selection/window did not scroll: selected=%d visible=%v", m.selected, m.visibleCompletions())
	}
}

func TestPromptModelPagesCompletions(t *testing.T) {
	m := newTestPromptModel(candidateNames("one", "two", "three", "four", "five", "six", "seven", "eight"))
	m = updateKeys(t, m, "d")
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.selected != 6 {
		t.Fatalf("page down selected = %d, want 6", m.selected)
	}
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.selected != 0 {
		t.Fatalf("page up selected = %d, want 0", m.selected)
	}

	closed := newPromptModel(promptModelOptions{History: []string{"first", "second"}})
	closed = updateKey(t, closed, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := closed.editor.Value(); got != "second" {
		t.Fatalf("closed completion up value = %q, want second", got)
	}

	closed = updateKey(t, closed, tea.WindowSizeMsg{Width: 80, Height: 12})
	closed.transcript = []string{"one", "two", "three", "four", "five", "six", "seven", "eight"}
	closed.syncViewportContent()
	closed = updateKey(t, closed, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if closed.viewport.AtBottom() {
		t.Fatal("closed completion page-up did not scroll the transcript")
	}
}

func TestPromptModelPagesPartialCompletionPage(t *testing.T) {
	m := newTestPromptModel(candidateNames("one", "two", "three"))
	m = updateKeys(t, m, "d")
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	original := m.selected

	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.selected != original {
		t.Fatalf("page down then up selected = %d, want original %d", m.selected, original)
	}
}

func TestPromptModelKeepsCompletionSelectionAcrossNonEditingMessages(t *testing.T) {
	m := newTestPromptModel(candidateNames("one", "two", "three"))
	m = updateKeys(t, m, "d")
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1 after down", m.selected)
	}

	m = updateKey(t, m, tea.FocusMsg{})
	if m.selected != 1 {
		t.Fatalf("selected = %d after non-editing message, want 1", m.selected)
	}
}

func TestPromptModelPreservesTableColumnSpacing(t *testing.T) {
	m := newTestPromptModel(nil)
	const row = "| NAME                                 | TDLIB PEER ID                        |"
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: row})

	if got := m.transcript; !reflect.DeepEqual(got, []string{row}) {
		t.Fatalf("transcript = %q, want table spacing preserved", got)
	}
}

func TestPromptModelRendersStableLayoutAtCommonWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			m := newTestPromptModel(candidateNames("one", "two", "three", "four", "five", "six", "seven"))
			m = updateKey(t, m, tea.WindowSizeMsg{Width: width, Height: 12})
			m = updateKeys(t, m, "d")

			view := m.render()
			if !strings.Contains(view, "tgdownloader") {
				t.Fatalf("header missing from view:\n%s", view)
			}
			if got := len(m.visibleCompletions()); got != 2 {
				t.Fatalf("visible completions = %d, want 2", got)
			}
			if m.viewport.Width() < 0 || m.viewport.Height() < 0 {
				t.Fatalf("viewport dimensions = %dx%d", m.viewport.Width(), m.viewport.Height())
			}
		})
	}
}

func TestPromptModelKeepsActiveRowsInEventOrder(t *testing.T) {
	m := newTestPromptModel(nil)
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressCreate, ID: "first", Label: "first row"})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressCreate, ID: "second", Label: "second row"})

	for i := 0; i < 100; i++ {
		view := m.render()
		if strings.Index(view, "first row") > strings.Index(view, "second row") {
			t.Fatalf("active rows rendered out of event order:\n%s", view)
		}
	}
}

func TestPromptModelRendersBarForMeasuredProgress(t *testing.T) {
	m := newTestPromptModel(nil)
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 80, Height: 12})
	m.applyRendererEvent(renderer.Event{
		Kind:    renderer.EventProgressUpdate,
		ID:      "video",
		Label:   "video.mp4",
		Current: 50,
		Total:   100,
		Unit:    renderer.ProgressUnitBytes,
	})

	view := m.render()
	if !strings.Contains(view, "video.mp4") {
		t.Fatalf("progress label missing:\n%s", view)
	}
	if !strings.Contains(view, "#") || !strings.Contains(view, ".") {
		t.Fatalf("measured progress has no ASCII bar:\n%s", view)
	}
	if strings.ContainsAny(view, "█░") {
		t.Fatalf("measured progress contains legacy bar characters:\n%s", view)
	}
}

func TestPromptModelAnimatesUnknownProgressWhileActive(t *testing.T) {
	m := newTestPromptModel(nil)
	m.activeRows["connect"] = renderer.Event{Kind: renderer.EventProgressUpdate, ID: "connect", Label: "Connecting"}

	updated, cmd := m.Update(promptProgressTickMsg{})
	m = updated.(*promptModel)
	if m.progressFrame != 1 {
		t.Fatalf("progress frame = %d, want 1", m.progressFrame)
	}
	if cmd == nil {
		t.Fatal("unknown progress did not schedule next animation tick")
	}
}

func TestPromptModelStopsProgressAnimationWithoutUnknownRows(t *testing.T) {
	m := newTestPromptModel(nil)
	m.progressTicking = true

	updated, cmd := m.Update(promptProgressTickMsg{})
	m = updated.(*promptModel)
	if m.progressTicking {
		t.Fatal("progress animation remained active without unknown rows")
	}
	if cmd != nil {
		t.Fatal("progress animation scheduled another tick without unknown rows")
	}
}

func TestPromptModelAlignsMeasuredProgressBars(t *testing.T) {
	m := newTestPromptModel(nil)
	const width = 72
	short := sanitizePromptModelLine(m.renderProgressRow(renderer.Event{
		Label: "a.mp4", Current: 50, Total: 100, Unit: renderer.ProgressUnitBytes,
	}, width))
	long := sanitizePromptModelLine(m.renderProgressRow(renderer.Event{
		Label: "a much longer video filename.mp4", Current: 50, Total: 100, Unit: renderer.ProgressUnitBytes,
	}, width))

	shortBar := lipgloss.Width(short[:strings.Index(short, "[")])
	longBar := lipgloss.Width(long[:strings.Index(long, "[")])
	if shortBar != longBar {
		t.Fatalf("progress bars start at different columns: short=%d long=%d\n%s\n%s", shortBar, longBar, short, long)
	}
}

func TestPromptModelDoesNotCaptureTerminalMouse(t *testing.T) {
	m := newTestPromptModel(nil)

	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("mouse mode = %v, want MouseModeNone so terminal selection works", got)
	}
}

func TestPromptModelPromotesDoneRowOnce(t *testing.T) {
	m := newTestPromptModel(nil)
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressCreate, ID: "work", Label: "work", Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressUpdate, ID: "work", Label: "work", Current: 1, Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressDone, ID: "work", Label: "work", Current: 1, Total: 1, Elapsed: time.Millisecond})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressDone, ID: "work", Label: "duplicate", Current: 1, Total: 1})

	if len(m.activeRows) != 0 || len(m.activeRowOrder) != 0 {
		t.Fatalf("active rows = %v order = %v, want empty", m.activeRows, m.activeRowOrder)
	}
	if len(m.transcript) != 1 || !strings.Contains(sanitizePromptModelLine(m.transcript[0]), "done! [1ms]") {
		t.Fatalf("transcript = %q, want one formatted done row", m.transcript)
	}
}

func TestPromptModelPromotesFailedRowOnce(t *testing.T) {
	m := newTestPromptModel(nil)
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressCreate, ID: "work", Label: "work", Total: 1})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressFail, ID: "work", Label: "work", Total: 1, Elapsed: 2 * time.Millisecond})
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventProgressUpdate, ID: "work", Label: "late", Total: 1})

	if len(m.activeRows) != 0 {
		t.Fatalf("active rows = %v, want empty", m.activeRows)
	}
	if len(m.transcript) != 1 || !strings.Contains(sanitizePromptModelLine(m.transcript[0]), "fail! [2ms]") {
		t.Fatalf("transcript = %q, want one formatted failure row", m.transcript)
	}
}

func TestPromptModelKeepsEditorWithinWindowWithActiveRows(t *testing.T) {
	m := newTestPromptModel(nil)
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 60, Height: 12})
	m.applyRendererEvent(renderer.Event{ID: "one", Text: "first row"})
	m.applyRendererEvent(renderer.Event{ID: "two", Text: "second row"})

	if got := len(strings.Split(m.render(), "\n")); got != 12 {
		t.Fatalf("rendered lines = %d, want 12", got)
	}
}

func TestPromptModelRendersExactHeightOnShortTerminals(t *testing.T) {
	for height := 1; height <= 9; height++ {
		t.Run(fmt.Sprintf("height_%d", height), func(t *testing.T) {
			m := newTestPromptModel(candidateNames("one", "two", "three"))
			m = updateKey(t, m, tea.WindowSizeMsg{Width: 60, Height: height})
			m = updateKeys(t, m, "d")

			if got := len(strings.Split(m.render(), "\n")); got != height {
				t.Fatalf("rendered lines = %d, want %d", got, height)
			}
		})
	}
}

func TestPromptModelPreservesTranscriptCommandAndHintAtHeightNine(t *testing.T) {
	m := newTestPromptModel(candidateNames("download"))
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 60, Height: 9})
	m.transcript = []string{"height-nine transcript"}
	m.syncViewportContent()
	m = updateKeys(t, m, "d")

	view := m.render()
	assertPromptLayout(t, view, 60, 9)
	if !strings.Contains(view, "height-nine transcript") {
		t.Fatalf("transcript missing at height 9:\n%s", view)
	}
	plainView := sanitizePromptModelText(view)
	if !strings.Contains(plainView, "tg>d") {
		t.Fatalf("command input missing at height 9:\n%s", view)
	}
	if !strings.Contains(view, "up/down select") {
		t.Fatalf("completion hint missing at height 9:\n%s", view)
	}
}

func TestPromptModelKeepsStableTranscriptRegionAtCommonWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			for _, transcript := range [][]string{
				nil,
				{"short transcript"},
				{"one", "two", "three", "four", "five"},
			} {
				m := newTestPromptModel(nil)
				m = updateKey(t, m, tea.WindowSizeMsg{Width: width, Height: 14})
				m.transcript = transcript
				assertPromptLayout(t, m.render(), width, 14)
			}
		})
	}
}

func TestPromptModelRendersBorderedZones(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			m := newTestPromptModel(candidateNames("one", "two", "three", "four", "five", "six", "seven", "eight"))
			m = updateKey(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
			m.transcript = []string{"output row"}
			m.syncViewportContent()
			m = updateKeys(t, m, "d")

			view := m.render()
			assertPromptLayout(t, view, width, 24)
			assertPromptPanel(t, view, "OUTPUT", width, 9)
			assertPromptPanel(t, view, "SUGGESTIONS 1/8", width, maxPromptVisibleCompletions)
			assertPromptPanel(t, view, "COMMAND", width, 1)
		})
	}
}

func TestPromptModelShrinksSuggestionsOnShortTerminal(t *testing.T) {
	tests := []struct {
		height         int
		suggestionRows int
		outputRows     int
	}{
		{height: 10, suggestionRows: 0, outputRows: 1},
		{height: 12, suggestionRows: 2, outputRows: 1},
		{height: 24, suggestionRows: maxPromptVisibleCompletions, outputRows: 9},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("height_%d", tt.height), func(t *testing.T) {
			m := newTestPromptModel(candidateNames("one", "two", "three", "four", "five", "six", "seven", "eight"))
			m = updateKey(t, m, tea.WindowSizeMsg{Width: 80, Height: tt.height})
			m.transcript = []string{"output row"}
			m.syncViewportContent()
			m = updateKeys(t, m, "d")

			view := m.render()
			assertPromptLayout(t, view, 80, tt.height)
			assertPromptPanel(t, view, "OUTPUT", 80, tt.outputRows)
			assertPromptPanel(t, view, "SUGGESTIONS 1/8", 80, tt.suggestionRows)
			assertPromptPanel(t, view, "COMMAND", 80, 1)
			if !strings.Contains(view, "output row") {
				t.Fatalf("output body row missing:\n%s", view)
			}
			if !strings.Contains(sanitizePromptModelText(view), "tg>d") {
				t.Fatalf("command input missing:\n%s", view)
			}
			if !strings.Contains(view, "up/down select") {
				t.Fatalf("completion hint missing:\n%s", view)
			}
			if got := m.visibleCompletionCount(); got != tt.suggestionRows {
				t.Fatalf("visible completion count = %d, want %d", got, tt.suggestionRows)
			}

			if tt.suggestionRows == 0 {
				m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
				if m.selected != 1 {
					t.Fatalf("collapsed selection = %d, want 1", m.selected)
				}
				m.completionEnd = 1
				m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
				if got := m.editor.Value(); got != "two" {
					t.Fatalf("accepted completion = %q, want two", got)
				}
			}
		})
	}
}

func TestPromptModelBoundsNarrowMultilinePresentation(t *testing.T) {
	m := newPromptModel(promptModelOptions{
		Username: "user\r\nname\twith a very long suffix",
		Version:  "version\nwith a very long suffix",
	})
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 8, Height: 12})
	m.applyRendererEvent(renderer.Event{ID: "progress", Text: "line one\r\nline\ttwo"})

	view := m.render()
	if strings.Contains(view, "\r") || strings.Contains(view, "\t") {
		t.Fatalf("view contains unsanitized control characters: %q", view)
	}
	assertPromptLayout(t, view, 8, 12)
}

func TestPromptModelFollowsOutputUntilUserScrolls(t *testing.T) {
	m := newTestPromptModel(nil)
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 60, Height: 12})
	for i := 1; i <= 8; i++ {
		m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: fmt.Sprintf("line-%02d", i)})
	}

	if view := m.render(); !strings.Contains(view, "line-08") || !m.viewport.AtBottom() {
		t.Fatalf("viewport did not follow initial output: offset=%d\n%s", m.viewport.YOffset(), view)
	}

	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	scrolledOffset := m.viewport.YOffset()
	if m.viewport.AtBottom() {
		t.Fatal("page-up did not leave the bottom of the transcript")
	}
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: "line-09"})
	if view := m.render(); strings.Contains(view, "line-09") || m.viewport.YOffset() != scrolledOffset {
		t.Fatalf("new output displaced a scrolled transcript: offset=%d want=%d\n%s", m.viewport.YOffset(), scrolledOffset, view)
	}

	for i := 0; i < 10 && !m.viewport.AtBottom(); i++ {
		m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("page-down did not resume bottom following: offset=%d", m.viewport.YOffset())
	}
	m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: "line-10"})
	if view := m.render(); !strings.Contains(view, "line-10") {
		t.Fatalf("viewport did not resume following new output:\n%s", view)
	}

	m = updateKey(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.AtBottom() {
		t.Fatal("mouse wheel did not scroll the transcript")
	}
}

func TestPromptModelScrollsOutputWithCtrlArrows(t *testing.T) {
	m := newTestPromptModel(nil)
	m = updateKey(t, m, tea.WindowSizeMsg{Width: 60, Height: 12})
	for i := 1; i <= 12; i++ {
		m.applyRendererEvent(renderer.Event{Kind: renderer.EventLine, Text: fmt.Sprintf("line-%02d", i)})
	}
	bottom := m.viewport.YOffset()

	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	if got := m.viewport.YOffset(); got != bottom-1 {
		t.Fatalf("ctrl+up offset = %d, want %d", got, bottom-1)
	}
	m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl})
	if got := m.viewport.YOffset(); got != bottom {
		t.Fatalf("ctrl+down offset = %d, want %d", got, bottom)
	}
	if hint := sanitizePromptModelText(m.renderHint()); !strings.Contains(hint, "ctrl+up/down scroll") {
		t.Fatalf("scroll shortcut missing from hint: %q", hint)
	}
}

func TestPromptModelSanitizesAllModelBoundText(t *testing.T) {
	m := newPromptModel(promptModelOptions{
		Username: "dialog\x1b[31m-name\x1b[0m\u202e",
		Version:  "v0.6.0\x1b]0;owned\a",
	})
	m.applyRendererEvent(renderer.Event{
		Kind: renderer.EventLine,
		Text: "dialog Safe\x1b[31m Red\x1b[0m\x1b]0;owned\a\u202e\x00 end",
	})
	m.applyRendererEvent(renderer.Event{
		Kind:  renderer.EventProgressCreate,
		ID:    "file",
		Label: "file Safe\u009b31m Red\u009b0m\u009d0;owned\a\u2066 end",
	})
	m.finishCommand(promptCommandDoneMsg{Err: errors.New("failure\x1b[2J\x1b]52;c;owned\a\u202e end")})

	view := m.render()
	for _, forbidden := range []string{"\x1b[31m", "\x1b[2J", "\x1b]", "\u009b", "\u009d", "\x00", "\a", "\u202e", "\u2066"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("view contains unsafe terminal text %q: %q", forbidden, view)
		}
	}
	for _, safe := range []string{"dialog Safe Red end", "file Safe Red end", "failure end"} {
		if !strings.Contains(view, safe) {
			t.Fatalf("sanitized text %q missing from view: %q", safe, view)
		}
	}
}

func TestPromptModelCompletionErrorIsOneStatusRowUsingLifetimeContext(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	var contexts []context.Context
	m := newPromptModel(promptModelOptions{
		Lifetime: lifetime,
		Complete: func(ctx context.Context, _ string, _ int) completionResult {
			contexts = append(contexts, ctx)
			return completionResult{Err: errors.New("dialog cache unavailable")}
		},
	})
	m = updateKeys(t, m, "abc")

	if len(m.transcript) != 0 {
		t.Fatalf("completion errors leaked into transcript: %q", m.transcript)
	}
	if got := strings.Count(m.render(), "dialog cache unavailable"); got != 1 {
		t.Fatalf("completion status count = %d, want 1\n%s", got, m.render())
	}
	for i, ctx := range contexts {
		if ctx != lifetime {
			t.Fatalf("completion context %d = %T, want prompt lifetime context", i, ctx)
		}
	}
}

func TestPromptModelRendersConnectedHeaderWithSingleVersionPrefix(t *testing.T) {
	m := newPromptModel(promptModelOptions{Username: "tester", Version: "v0.6.0", Connected: true})
	header := strings.Split(m.render(), "\n")[0]
	if !strings.Contains(header, "connected") {
		t.Fatalf("connection state missing from header: %q", header)
	}
	if !strings.Contains(header, "v0.6.0") || strings.Contains(header, "vv0.6.0") {
		t.Fatalf("version prefix is incorrect: %q", header)
	}
}

func TestPromptModelRendersWrappedExpectedErrorOnceAndConcise(t *testing.T) {
	m := newTestPromptModel(nil)
	err := apperr.New("cmd.download.stop", apperr.KindNetwork, errors.New("download failed"))
	m.finishCommand(promptCommandDoneMsg{Err: err})

	view := m.render()
	if got := strings.Count(view, "Error: download failed"); got != 1 {
		t.Fatalf("concise error count = %d, want 1: %q", got, view)
	}
	if strings.Contains(view, "cmd.download.stop") || strings.Contains(view, string(apperr.KindNetwork)) {
		t.Fatalf("expected error exposed internal details: %q", view)
	}
}

func assertPromptLayout(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if got := len(lines); got != height {
		t.Fatalf("rendered lines = %d, want %d:\n%s", got, height, view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line width = %d, want <= %d: %q", got, width, line)
		}
	}
}

func assertPromptPanel(t *testing.T, view, title string, width, bodyRows int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, title) {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("panel title %q missing:\n%s", title, view)
	}
	bottom := top + bodyRows + 1
	if bottom >= len(lines) {
		t.Fatalf("panel %q bottom index = %d, rendered lines = %d", title, bottom, len(lines))
	}
	if !strings.Contains(lines[top], "┌") || !strings.Contains(lines[top], "┐") {
		t.Fatalf("panel %q top border missing: %q", title, lines[top])
	}
	if !strings.Contains(lines[bottom], "└") || !strings.Contains(lines[bottom], "┘") {
		t.Fatalf("panel %q bottom border missing: %q", title, lines[bottom])
	}
	if got := lipgloss.Width(lines[top]); got != width {
		t.Fatalf("panel %q top width = %d, want %d", title, got, width)
	}
	if got := lipgloss.Width(lines[bottom]); got != width {
		t.Fatalf("panel %q bottom width = %d, want %d", title, got, width)
	}
	for i := top + 1; i < bottom; i++ {
		if got := lipgloss.Width(lines[i]); got != width {
			t.Fatalf("panel %q body line %d width = %d, want %d: %q", title, i-top, got, width, lines[i])
		}
	}
}

func newTestPromptModel(candidates []promptCandidate) *promptModel {
	return newPromptModel(promptModelOptions{
		Username: "tester",
		Version:  "test",
		Complete: func(context.Context, string, int) completionResult {
			return completionResult{Candidates: candidates}
		},
		Submit: func(context.Context, string) tea.Cmd { return nil },
	})
}

func candidateNames(names ...string) []promptCandidate {
	candidates := make([]promptCandidate, len(names))
	for i, name := range names {
		candidates[i] = promptCandidate{Value: name, Display: name}
	}
	return candidates
}

func updateKeys(t *testing.T, m *promptModel, keys string) *promptModel {
	t.Helper()
	for _, key := range keys {
		m = updateKey(t, m, tea.KeyPressMsg{Code: key, Text: string(key)})
	}
	return m
}

func updateKey(t *testing.T, m *promptModel, msg tea.Msg) *promptModel {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(*promptModel)
	if !ok {
		t.Fatalf("model type = %T, want *promptModel", updated)
	}
	return model
}
