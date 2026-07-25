package cmd

import (
	"strings"
	"testing"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
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

func TestPeerCandidateMatchesSubstringAndPreservesFullName(t *testing.T) {
	name := "Очень длинное название канала с фотографиями внутреннего танца и окончанием"
	candidate, ok := peerCandidate(cachedChannel(1, name), "фото")
	if !ok {
		t.Fatal("expected substring match")
	}
	if candidate.Value != name {
		t.Fatalf("value = %q, want full name", candidate.Value)
	}
	if candidate.Display != name {
		t.Fatalf("display = %q, want full name", candidate.Display)
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
