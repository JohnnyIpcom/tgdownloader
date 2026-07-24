package cmd

import (
	"context"
	"strings"
	"testing"

	prompt "github.com/c-bata/go-prompt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

func cachedChannel(id int64, title string) telegram.DialogPeer {
	return telegram.DialogPeer{Peer: storage.Peer{
		Key:     dialogs.DialogKey{Kind: dialogs.Channel, ID: id},
		Channel: &tg.Channel{ID: id, Title: title},
	}}
}

func cachedUser(id int64, firstName, username string) telegram.DialogPeer {
	return telegram.DialogPeer{Peer: storage.Peer{
		Key:  dialogs.DialogKey{Kind: dialogs.User, ID: id},
		User: &tg.User{ID: id, FirstName: firstName, Username: username},
	}}
}

func TestDialogPeerSuggestMatchesNameInput(t *testing.T) {
	peer := cachedChannel(123, "Cherry Channel")

	suggest, ok := dialogPeerSuggest(peer, "che")
	if !ok {
		t.Fatal("expected suggestion")
	}

	if suggest.Text != "Cherry Channel" {
		t.Fatalf("expected name text, got %q", suggest.Text)
	}

	if suggest.Description != "" {
		t.Fatalf("expected empty description, got %q", suggest.Description)
	}
}

func TestCompleterUsesRootContextForPeerSuggestions(t *testing.T) {
	type contextKey struct{}
	wantCtx := context.WithValue(context.Background(), contextKey{}, "prompt")
	cache := &dialogCacheStub{}
	r := &Root{client: &telegram.Client{DialogCache: cache}}

	rootCmd := &cobra.Command{Use: "tgdownloader"}
	downloadCmd := &cobra.Command{Use: "download"}
	historyCmd := &cobra.Command{
		Use: "history",
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
	}
	downloadCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(downloadCmd)
	rootCmd.SetContext(wantCtx)

	buffer := prompt.NewBuffer()
	buffer.InsertText("download history che", false, true)
	r.newCompleter(rootCmd)(*buffer.Document())

	if cache.ctx != wantCtx {
		t.Fatalf("expected root prompt context, got %v", cache.ctx)
	}
}

func TestDialogPeerSuggestSearchesUsernameButDisplaysVisibleName(t *testing.T) {
	peer := cachedUser(7, "_anastasiia_", "lscptd")

	suggest, ok := dialogPeerSuggest(peer, "lsc")
	if !ok {
		t.Fatal("expected username alias suggestion")
	}
	if suggest.Text != "_anastasiia_" {
		t.Fatalf("expected visible name, got %q", suggest.Text)
	}
}

func TestResolveDialogPeerByUsernameAlias(t *testing.T) {
	peer := cachedUser(7, "_anastasiia_", "lscptd")

	got, err := resolveDialogPeerByInput([]telegram.DialogPeer{peer}, "lscptd")
	if err != nil {
		t.Fatal(err)
	}
	if got.TDLibPeerID() != peer.TDLibPeerID() {
		t.Fatalf("expected %v, got %v", peer.TDLibPeerID(), got.TDLibPeerID())
	}
}

func TestPeerInputArgJoinsMultiWordName(t *testing.T) {
	got := peerInputArg([]string{"Фотограф", "внутреннего", "танца"})
	if got != "Фотограф внутреннего танца" {
		t.Fatalf("expected joined peer input, got %q", got)
	}
}

func TestPeerInputArgsRequiresInput(t *testing.T) {
	cmd := &cobra.Command{Use: "history"}
	if err := peerInputArgs(cmd, []string{}); err == nil {
		t.Fatal("expected missing input error")
	}
	if err := peerInputArgs(cmd, []string{"Фотограф", "внутреннего", "танца"}); err != nil {
		t.Fatalf("expected multi-word input to be accepted, got %v", err)
	}
}

func TestDialogPeerSuggestMatchesIDInput(t *testing.T) {
	peer := cachedChannel(123, "Cherry Channel")
	id := renderer.RenderTDLibPeerID(peer.TDLibPeerID())

	suggest, ok := dialogPeerSuggest(peer, strings.ToLower(id[:6]))
	if !ok {
		t.Fatal("expected suggestion")
	}

	if suggest.Text != id {
		t.Fatalf("expected ID text, got %q", suggest.Text)
	}
	if suggest.Description != "" {
		t.Fatalf("expected empty description, got %q", suggest.Description)
	}
}

func TestDialogPeerSuggestSkipsEmptyNameInNameMode(t *testing.T) {
	peer := cachedChannel(123, "")

	if suggest, ok := dialogPeerSuggest(peer, "nat"); ok {
		t.Fatalf("expected empty-name peer to be skipped, got %+v", suggest)
	}
	if suggest, ok := dialogPeerSuggest(peer, ""); ok {
		t.Fatalf("expected empty-name peer to be skipped for empty query, got %+v", suggest)
	}
	if suggest, ok := dialogPeerSuggest(peer, `"`); ok {
		t.Fatalf("expected empty-name peer to be skipped for quote query, got %+v", suggest)
	}
}

func TestDialogPeerSuggestLimitsLongNames(t *testing.T) {
	longName := "very long channel name with more than forty eight visible cells and suffix"
	peer := cachedChannel(123, longName)

	suggest, ok := dialogPeerSuggest(peer, "very")
	if !ok {
		t.Fatal("expected suggestion")
	}
	if runewidth.StringWidth(suggest.Text) > maxPromptPeerSuggestionWidth {
		t.Fatalf("expected suggestion width <= %d, got %d for %q", maxPromptPeerSuggestionWidth, runewidth.StringWidth(suggest.Text), suggest.Text)
	}
	if !strings.HasPrefix(longName, suggest.Text) {
		t.Fatalf("expected truncated suggestion to remain a prefix, got %q", suggest.Text)
	}
}

func TestDialogPeerSuggestSanitizesControlWhitespace(t *testing.T) {
	peer := cachedChannel(123, "Cherry\nChannel\tName")

	suggest, ok := dialogPeerSuggest(peer, "cher")
	if !ok {
		t.Fatal("expected suggestion")
	}
	if suggest.Text != "Cherry Channel Name" {
		t.Fatalf("expected sanitized suggestion, got %q", suggest.Text)
	}
}

func TestDialogPeerSuggestDropsSymbolsAndMarks(t *testing.T) {
	peer := cachedChannel(123, "Kharkiv Office Only \u00a4\ufe0f")

	suggest, ok := dialogPeerSuggest(peer, "khark")
	if !ok {
		t.Fatal("expected suggestion")
	}
	if suggest.Text != "Kharkiv Office Only" {
		t.Fatalf("expected safe suggestion, got %q", suggest.Text)
	}
}

func TestResolveDialogPeerByInputUsesSanitizedPrefix(t *testing.T) {
	peers := []telegram.DialogPeer{
		cachedChannel(123, "Cherry\nChannel\tName"),
	}

	got, err := resolveDialogPeerByInput(peers, "Cherry Channel")
	if err != nil {
		t.Fatalf("expected sanitized prefix match, got %v", err)
	}
	if got.TDLibPeerID() != peers[0].TDLibPeerID() {
		t.Fatalf("expected first peer, got %v", got.TDLibPeerID())
	}
}

func TestResolveDialogPeerByInputUsesUniquePrefix(t *testing.T) {
	peers := []telegram.DialogPeer{
		cachedChannel(123, "Cherry Channel"),
		cachedChannel(456, "Other Channel"),
	}

	got, err := resolveDialogPeerByInput(peers, "cher")
	if err != nil {
		t.Fatalf("expected unique prefix match, got %v", err)
	}

	if got.TDLibPeerID() != peers[0].TDLibPeerID() {
		t.Fatalf("expected first peer, got %v", got.TDLibPeerID())
	}
}

func TestResolveDialogPeerByInputRejectsAmbiguousPrefix(t *testing.T) {
	peers := []telegram.DialogPeer{
		cachedChannel(123, "Cherry One"),
		cachedChannel(456, "Cherry Two"),
	}

	_, err := resolveDialogPeerByInput(peers, "cher")
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "ambiguous peer name") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Cherry One") || !strings.Contains(err.Error(), "Cherry Two") {
		t.Fatalf("expected candidates in error, got %v", err)
	}
}
