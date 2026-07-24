package telegram

import (
	"context"
	"testing"

	messagepeer "github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

func TestDialogServiceSendDialogCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan Dialog)
	s := &dialogService{}
	if err := s.sendDialog(ctx, out, Dialog{}); err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestDialogPeerUsesBatchEntities(t *testing.T) {
	tests := []struct {
		name string
		elem dialogs.Elem
		want string
	}{
		{
			name: "user",
			elem: dialogs.Elem{
				Dialog: &tg.Dialog{Peer: &tg.PeerUser{UserID: 1}},
				Entities: messagepeer.NewEntities(
					map[int64]*tg.User{1: {ID: 1, FirstName: "Visible", LastName: "User"}},
					nil,
					nil,
				),
			},
			want: "Visible User",
		},
		{
			name: "chat",
			elem: dialogs.Elem{
				Dialog: &tg.Dialog{Peer: &tg.PeerChat{ChatID: 2}},
				Entities: messagepeer.NewEntities(
					nil,
					map[int64]*tg.Chat{2: {ID: 2, Title: "Chat", Photo: &tg.ChatPhotoEmpty{}}},
					nil,
				),
			},
			want: "Chat",
		},
		{
			name: "channel",
			elem: dialogs.Elem{
				Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 3}},
				Entities: messagepeer.NewEntities(
					nil,
					nil,
					map[int64]*tg.Channel{3: {ID: 3, Title: "Channel", Photo: &tg.ChatPhotoEmpty{}}},
				),
			},
			want: "Channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer, ok := dialogPeer(tt.elem)
			if !ok {
				t.Fatal("expected dialog peer")
			}
			if got := (DialogPeer{Peer: peer}).Name(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDialogPeerRejectsMissingEntity(t *testing.T) {
	elem := dialogs.Elem{
		Dialog:   &tg.Dialog{Peer: &tg.PeerUser{UserID: 1}},
		Entities: messagepeer.NewEntities(nil, nil, nil),
	}

	if _, ok := dialogPeer(elem); ok {
		t.Fatal("expected missing entity to be rejected")
	}
}
