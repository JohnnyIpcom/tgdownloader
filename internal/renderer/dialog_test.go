package renderer

import (
	"strings"
	"testing"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

func TestRenderDialogsTableShowsDialogFieldsOnly(t *testing.T) {
	peer := telegram.DialogPeer{Peer: storage.Peer{
		Key:     dialogs.DialogKey{Kind: dialogs.Channel, ID: 123, AccessHash: 456},
		Channel: &tg.Channel{ID: 123, Title: "Cherry Channel"},
	}}

	got := RenderDialogsTable([]telegram.DialogPeer{peer})
	for _, want := range []string{"NAME", "ID", "TDLIB PEER ID", "TYPE", "Cherry Channel", "Channel"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected table to contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ACCESS HASH") || strings.Contains(got, "456") {
		t.Fatalf("expected dialog table to hide access hash:\n%s", got)
	}
}
