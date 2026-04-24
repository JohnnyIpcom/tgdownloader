package telegram

import (
	"context"
	"strings"
	"testing"

	telegrammocks "github.com/johnnyipcom/tgdownloader/pkg/telegram/mocks"
	"go.uber.org/mock/gomock"
)

func TestParseMessageLinkCommentRequiresBroadcastChannel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	commentPeer := telegrammocks.NewMocklinkedChatPeer(ctrl)
	commentPeer.EXPECT().IsBroadcast().Return(false)

	peerSvc := telegrammocks.NewMockPeerService(ctrl)
	peerSvc.EXPECT().Resolve(gomock.Any(), "example").Return(commentPeer, nil)

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
