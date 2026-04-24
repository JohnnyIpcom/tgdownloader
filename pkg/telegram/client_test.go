package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"go.uber.org/mock/gomock"
)

type stubCommentPeer struct {
	broadcast bool
}

func (s stubCommentPeer) ID() int64 { return 0 }

func (s stubCommentPeer) TDLibPeerID() constant.TDLibPeerID {
	var id constant.TDLibPeerID
	return id
}

func (s stubCommentPeer) VisibleName() string { return "" }

func (s stubCommentPeer) Username() (string, bool) { return "", false }

func (s stubCommentPeer) Restricted() ([]tg.RestrictionReason, bool) { return nil, false }

func (s stubCommentPeer) Verified() bool { return false }

func (s stubCommentPeer) Scam() bool { return false }

func (s stubCommentPeer) Fake() bool { return false }

func (s stubCommentPeer) InputPeer() tg.InputPeerClass { return &tg.InputPeerEmpty{} }

func (s stubCommentPeer) Sync(context.Context) error { return nil }

func (s stubCommentPeer) Manager() *peers.Manager { return nil }

func (s stubCommentPeer) Report(context.Context, tg.ReportReasonClass, string) error { return nil }

func (s stubCommentPeer) Photo(context.Context) (*tg.Photo, bool, error) { return nil, false, nil }

func (s stubCommentPeer) IsBroadcast() bool { return s.broadcast }

func (s stubCommentPeer) FullRaw(context.Context) (*tg.ChannelFull, error) {
	return &tg.ChannelFull{}, nil
}

func TestParseMessageLinkCommentRequiresBroadcastChannel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	peerSvc := NewMockPeerService(ctrl)
	peerSvc.EXPECT().Resolve(gomock.Any(), "example").Return(stubCommentPeer{broadcast: false}, nil)

	client := &Client{}
	client.PeerService = peerSvc

	peer, msgID, err := client.ParseMessageLink(context.Background(), "https://t.me/example/4434?comment=360409")
	if err == nil {
		t.Fatalf("expected error, got nil with peer=%v msgID=%d", peer, msgID)
	}

	if !strings.Contains(err.Error(), "broadcast channel") {
		t.Fatalf("expected broadcast channel error, got %v", err)
	}
}
