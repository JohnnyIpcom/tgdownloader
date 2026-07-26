package cmd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
)

func rootWithDialogPeers(peers ...telegram.DialogPeer) *Root {
	return &Root{client: &telegram.Client{DialogCache: &dialogCacheStub{peers: peers}}}
}

func TestCompletePromptSuggestsRootAndNestedCommands(t *testing.T) {
	r := &Root{}

	rootResult := r.completePrompt(context.Background(), "d", 1)
	rootValues := make([]string, len(rootResult.Candidates))
	for i, candidate := range rootResult.Candidates {
		rootValues[i] = candidate.Value
	}
	if want := []string{"dialog", "download"}; !reflect.DeepEqual(rootValues, want) {
		t.Fatalf("root candidates = %q, want %q", rootValues, want)
	}

	nestedResult := r.completePrompt(context.Background(), "download h", len("download h"))
	if len(nestedResult.Candidates) != 1 || nestedResult.Candidates[0].Value != "history" {
		t.Fatalf("nested candidates = %+v, want history", nestedResult.Candidates)
	}
}

func TestCompletePromptReturnsAllPeerSubstringsWithoutTab(t *testing.T) {
	r := rootWithDialogPeers(
		cachedChannel(1, "Фотограф внутреннего танца"),
		cachedChannel(2, "Фотоархив"),
		cachedChannel(3, "Офисное фото"),
	)

	line := "download history фо"
	result := r.completePrompt(context.Background(), line, len([]rune(line)))

	if result.Err != nil || len(result.Candidates) != 3 {
		t.Fatalf("result = %+v, want three substring matches", result)
	}
}

func TestCompletePromptRanksExactPrefixAndSubstring(t *testing.T) {
	r := rootWithDialogPeers(
		cachedChannel(1, "Office Cherry Archive"),
		cachedChannel(2, "Cherry Team"),
		cachedChannel(3, "Cherry"),
	)

	line := "download history cherry"
	result := r.completePrompt(context.Background(), line, len([]rune(line)))

	if result.Err != nil {
		t.Fatalf("complete prompt: %v", result.Err)
	}
	got := make([]string, len(result.Candidates))
	for i, candidate := range result.Candidates {
		got[i] = candidate.Value
	}

	want := []string{"Cherry", "Cherry Team", "Office Cherry Archive"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate order = %q, want %q", got, want)
	}
}

func TestCompletePromptPreservesLongPeerNamesForWideFormattingAndOrdering(t *testing.T) {
	commonPrefix := strings.Repeat("shared-peer-", 5)
	alphaName := commonPrefix + "Alpha"
	zuluName := commonPrefix + "Zulu"
	r := rootWithDialogPeers(
		cachedChannel(1, zuluName),
		cachedChannel(2, alphaName),
	)
	line := "download history shared"

	result := r.completePrompt(context.Background(), line, len([]rune(line)))
	if result.Err != nil {
		t.Fatalf("complete prompt: %v", result.Err)
	}
	if got := len(result.Candidates); got != 2 {
		t.Fatalf("candidate count = %d, want 2", got)
	}

	want := []string{alphaName, zuluName}
	for i, candidate := range result.Candidates {
		if candidate.Value != want[i] || candidate.Display != want[i] {
			t.Fatalf("candidate %d = %+v, want full name %q", i, candidate, want[i])
		}
		if row := formatPromptCandidate(candidate, 120); !strings.Contains(row, want[i]) {
			t.Fatalf("width-120 row %d = %q, want full name %q", i, row, want[i])
		}
	}
}

func TestCompletePromptRetainsAllPeerMatches(t *testing.T) {
	r := rootWithDialogPeers(
		cachedChannel(1, "Cherry"),
		cachedChannel(2, "Cherry Alpha"),
		cachedChannel(3, "Cherry Bravo"),
		cachedChannel(4, "Cherry Charlie"),
		cachedChannel(5, "Cherry Delta"),
		cachedChannel(6, "Cherry Echo"),
		cachedChannel(7, "Office Cherry Archive"),
		cachedChannel(8, "Office Cherry Source"),
	)
	line := "download history cherry"
	result := r.completePrompt(context.Background(), line, len([]rune(line)))
	if result.Err != nil {
		t.Fatalf("complete prompt: %v", result.Err)
	}
	if got := len(result.Candidates); got != 8 {
		t.Fatalf("candidate count = %d, want 8", got)
	}

	got := make([]string, len(result.Candidates))
	for i, candidate := range result.Candidates {
		got[i] = candidate.Value
	}
	want := []string{"Cherry", "Cherry Alpha", "Cherry Bravo", "Cherry Charlie", "Cherry Delta", "Cherry Echo", "Office Cherry Archive", "Office Cherry Source"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate order = %q, want %q", got, want)
	}
}

func TestCompletePromptRetainsAllCommandMatches(t *testing.T) {
	r := &Root{promptRootFactory: func() *cobra.Command {
		root := &cobra.Command{Use: "tgdownloader"}
		for _, name := range []string{"cache", "cancel", "catalog", "check", "config", "connect", "copy", "create"} {
			root.AddCommand(&cobra.Command{Use: name, Short: name, Run: func(*cobra.Command, []string) {}})
		}
		return root
	}}

	result := r.completePrompt(context.Background(), "c", 1)
	if got := len(result.Candidates); got != 8 {
		t.Fatalf("candidate count = %d, want 8", got)
	}
}

func TestCompletePromptCompletesTDLibIDPrefixes(t *testing.T) {
	peer := cachedChannel(123, "Cherry Channel")
	id := renderer.RenderTDLibPeerID(peer.TDLibPeerID())
	r := rootWithDialogPeers(peer)
	line := "download history " + id[:6]

	result := r.completePrompt(context.Background(), line, len([]rune(line)))

	if result.Err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result = %+v, want one ID candidate", result)
	}
	if result.Candidates[0].Value != id {
		t.Fatalf("value = %q, want ID %q", result.Candidates[0].Value, id)
	}
}

func TestCompletePromptPreservesEmojiAndFullID(t *testing.T) {
	peer := cachedChannel(123, "🍒🍒🍒")
	fullID := renderer.RenderTDLibPeerID(peer.TDLibPeerID())
	r := rootWithDialogPeers(peer)
	line := "download history 🍒"

	result := r.completePrompt(context.Background(), line, len([]rune(line)))
	if result.Err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result = %+v, want one candidate", result)
	}
	candidate := result.Candidates[0]
	if candidate.Display != "🍒🍒🍒" || !strings.Contains(candidate.Description, fullID) {
		t.Fatalf("candidate = %+v, want emoji display and full ID %q", candidate, fullID)
	}
}

func TestFormatPromptPeerCandidateAlignsColumns(t *testing.T) {
	const width = 72
	const userID = "0x0000000000000001"
	const channelID = "0xFFFFFF0000000002"
	user := formatPromptCandidate(promptCandidate{
		Display: "Ann", Description: "User | " + userID,
	}, width)
	channel := formatPromptCandidate(promptCandidate{
		Display: "A much longer channel name", Description: "Channel | " + channelID,
	}, width)
	flag := formatPromptCandidate(promptCandidate{
		Display: "Cyber Dark 🇺🇦", Description: "Channel | " + channelID,
	}, width)
	column := func(row, field string) int {
		return lipgloss.Width(row[:strings.Index(row, field)])
	}

	if userType, channelType := column(user, "User"), column(channel, "Channel"); userType != channelType {
		t.Fatalf("type columns differ: user=%d channel=%d\n%s\n%s", userType, channelType, user, channel)
	}
	if userIDColumn, channelIDColumn := column(user, userID), column(channel, channelID); userIDColumn != channelIDColumn {
		t.Fatalf("ID columns differ: user=%d channel=%d\n%s\n%s", userIDColumn, channelIDColumn, user, channel)
	}
	if flagType := column(flag, "Channel"); flagType != column(channel, "Channel") {
		t.Fatalf("flag type column = %d, want %d\n%s\n%s", flagType, column(channel, "Channel"), flag, channel)
	}
	if got := lipgloss.Width(user); got != width {
		t.Fatalf("user row width = %d, want %d: %q", got, width, user)
	}
	if got := lipgloss.Width(channel); got != width {
		t.Fatalf("channel row width = %d, want %d: %q", got, width, channel)
	}
	if got := lipgloss.Width(flag); got != width {
		t.Fatalf("flag row width = %d, want %d: %q", got, width, flag)
	}
}

func TestCompletePromptReplacesOnlyQuotedActiveArgumentWithRuneOffsets(t *testing.T) {
	name := "Фотограф внутреннего танца"
	r := rootWithDialogPeers(cachedChannel(1, name))
	line := `download history "Фотограф внут"`

	result := r.completePrompt(context.Background(), line, len([]rune(line)))

	if result.Err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result = %+v, want one candidate", result)
	}
	if result.Candidates[0].Value != name {
		t.Fatalf("value = %q, want %q", result.Candidates[0].Value, name)
	}
	if result.Start != len([]rune(`download history "`)) || result.End != len([]rune(line))-1 {
		t.Fatalf("offsets = %d:%d, want active quoted argument", result.Start, result.End)
	}
}

func TestPromptCompletionInsertionEscapesQuotesOutsideQuotedArgument(t *testing.T) {
	assertExecutablePromptCompletion(t, `download history Chan`, `Channel "One"`)
}

func TestPromptCompletionInsertionEscapesQuotesInsideQuotedArgument(t *testing.T) {
	assertExecutablePromptCompletion(t, `download history "Chan"`, `Channel "One"`)
}

func TestPromptCompletionInsertionClosesUnterminatedQuotedArgument(t *testing.T) {
	assertExecutablePromptCompletion(t, `download history "Chan`, `Channel "One"`)
}

func assertExecutablePromptCompletion(t *testing.T, line, name string) {
	t.Helper()
	r := rootWithDialogPeers(cachedChannel(1, name))
	m := newPromptModel(promptModelOptions{Complete: r.completePrompt})
	m.editor.SetValue(line)
	m.editor.CursorEnd()
	m.refreshCompletions()
	if len(m.completions) != 1 {
		t.Fatalf("completion count = %d, want 1 for %q", len(m.completions), line)
	}
	m.acceptCompletion()

	args, err := splitPromptLine(m.editor.Value())
	if err != nil {
		t.Fatalf("split inserted line %q: %v", m.editor.Value(), err)
	}
	if want := []string{"download", "history", name}; !reflect.DeepEqual(args, want) {
		t.Fatalf("inserted line %q split to %q, want %q", m.editor.Value(), args, want)
	}
}

func TestCompletePromptPropagatesDialogCacheErrors(t *testing.T) {
	wantErr := errors.New("cache unavailable")
	r := &Root{client: &telegram.Client{DialogCache: dialogCacheErrorStub{err: wantErr}}}
	line := "download history che"
	result := r.completePrompt(context.Background(), line, len([]rune(line)))
	if !errors.Is(result.Err, wantErr) {
		t.Fatalf("error = %v, want %v", result.Err, wantErr)
	}
}

type dialogCacheErrorStub struct {
	err error
}

func (s dialogCacheErrorStub) GetDialogPeers(context.Context, ...telegram.DialogPeerFilter) ([]telegram.DialogPeer, error) {
	return nil, s.err
}

func TestSanitizePromptPeerNameRemovesTerminalAndBidiControls(t *testing.T) {
	got := sanitizePromptPeerName("Safe\x1b[31m Red\x1b[0m\nName\u202e!")
	if got != "Safe Red Name!" {
		t.Fatalf("sanitized name = %q", got)
	}
}

func TestSanitizePromptPeerNamePreservesEmojiAndCombiningMarks(t *testing.T) {
	const name = "🍒🍒🍒 Cafe\u0301 👨‍👩‍👧‍👦"
	if got := sanitizePromptPeerName(name); got != name {
		t.Fatalf("sanitized name = %q, want %q", got, name)
	}
}
