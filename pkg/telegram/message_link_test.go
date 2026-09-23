package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	telegrammocks "github.com/johnnyipcom/tgdownloader/pkg/telegram/mocks"
	"go.uber.org/mock/gomock"
)

func TestParsePrivateMessageLinkUsesStoredChannel(t *testing.T) {
	const (
		channelID  = int64(3716475718)
		accessHash = int64(123456789)
	)

	links := []string{
		"https://t.me/c/3716475718/499?single&thread=483",
		"https://t.me/c/3716475718/499?thread=483&single",
		"https://t.me/c/3716475718/499",
		"https://t.me/c/3716475718/483/499?single",
	}

	for _, link := range links {
		t.Run(link, func(t *testing.T) {
			service := newStoredPeerService(t, storage.Peer{
				Version: storage.LatestVersion,
				Key: dialogs.DialogKey{
					Kind: dialogs.Channel, ID: channelID, AccessHash: accessHash,
				},
				Channel: &tg.Channel{
					ID: channelID, AccessHash: accessHash,
					Title: "Private forum", Photo: &tg.ChatPhotoEmpty{},
				},
			})

			// A cold manager must use the persisted channel, not query an ID
			// without its access hash or fall back to a user with the same ID.
			client := service.client
			client.peerMgr = peers.Options{}.Build(tg.NewClient(errorInvoker{err: errors.New("unexpected network lookup")}))
			client.PeerService = service

			peer, messageID, err := client.ParseMessageLink(context.Background(), link)
			if err != nil {
				t.Fatal(err)
			}

			if messageID != 499 {
				t.Fatalf("message ID = %d, want 499 (not thread 483)", messageID)
			}

			input, ok := peer.InputPeer().(*tg.InputPeerChannel)
			if !ok || input.ChannelID != channelID || input.AccessHash != accessHash {
				t.Fatalf("input peer = %#v", peer.InputPeer())
			}
		})
	}
}

func TestParseMessageLinkPreservesChannelError(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := telegrammocks.NewMockPeerService(ctrl)
	wantErr := errors.New("CHANNEL_PRIVATE")
	service.EXPECT().ResolveTDLibID(gomock.Any(), channelTDLibID(3716475718)).Return(nil, wantErr)

	client := &Client{PeerService: service}
	_, _, err := client.ParseMessageLink(context.Background(), "https://t.me/c/3716475718/499?single&thread=483")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestParsePrivateMessageLinkRejectsInvalidIDs(t *testing.T) {
	for _, link := range []string{
		"https://t.me/c/not-a-channel/499",
		"https://t.me/c/0/499",
		"https://t.me/c/-3716475718/499",
		"https://t.me/c/9223372036854775807/499",
		"https://t.me/c/3716475718/not-a-message",
		"https://t.me/c/3716475718/0",
		"https://t.me/c/3716475718/-499",
	} {
		t.Run(link, func(t *testing.T) {
			// No resolver is configured: invalid IDs must fail before lookup.
			client := &Client{}
			if _, _, err := client.ParseMessageLink(context.Background(), link); err == nil {
				t.Fatal("expected invalid ID error")
			}
		})
	}
}

func TestParsePublicMessageLinks(t *testing.T) {
	for _, link := range []string{
		"https://t.me/example/499?single&thread=483",
		"https://t.me/example/483/499",
	} {
		t.Run(link, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			service := telegrammocks.NewMockPeerService(ctrl)
			wantPeer := telegrammocks.NewMocklinkedChatPeer(ctrl)
			service.EXPECT().Resolve(gomock.Any(), "example").Return(wantPeer, nil)

			client := &Client{PeerService: service}
			peer, messageID, err := client.ParseMessageLink(context.Background(), link)
			if err != nil || peer != wantPeer || messageID != 499 {
				t.Fatalf("peer=%v message=%d error=%v", peer, messageID, err)
			}
		})
	}
}
