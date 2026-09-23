package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestResolveDiscussionFromFreshDialogs(t *testing.T) {
	service, api, progress := newDiscussionTestService(t)
	client := service.client
	client.PeerService = service

	peer, messageID, err := client.ParseMessageLink(context.Background(), "https://t.me/c/3716475718/499?single&thread=483")
	if err != nil {
		t.Fatal(err)
	}

	input := peer.InputPeer().(*tg.InputPeerChannel)
	if input.ChannelID != api.discussion.ID || input.AccessHash != api.discussion.AccessHash || messageID != 499 {
		t.Fatalf("resolved %v, message %d", input, messageID)
	}

	if api.fullCalls != 1 || api.dialogCalls == 0 || len(progress.done) != 1 {
		t.Fatalf("full=%d dialogs=%d progress=%+v", api.fullCalls, api.dialogCalls, progress)
	}

	stored, err := client.storage.Find(context.Background(), storage.PeerKey{Kind: dialogs.Channel, ID: api.discussion.ID})
	if err != nil || stored.Channel == nil || stored.Key.AccessHash != api.discussion.AccessHash {
		t.Fatalf("persisted discussion = %+v, err=%v", stored, err)
	}

	// A new manager simulates restart: the durable entity must be sufficient.
	client.peerMgr = peers.Options{}.Build(tg.NewClient(errorInvoker{err: errors.New("unexpected network request")}))
	if _, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID)); err != nil {
		t.Fatalf("resolve after restart: %v", err)
	}
}

func TestResolveDiscussionDoesNotChangeDialogMembership(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)
	var parent storage.Peer
	parent.FromChat(api.parent)

	service.client.dialogCache = &dialogCache{}
	service.client.dialogCache.replaceMemory([]storage.Peer{parent})

	if _, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID)); err != nil {
		t.Fatal(err)
	}

	if len(service.client.dialogCache.snapshot()) != 1 {
		t.Fatal("discussion added to dialog membership")
	}
}

func TestResolveDiscussionPrioritizesReplyChannel(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)
	api.includeUnrelated = true

	if _, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID)); err != nil {
		t.Fatal(err)
	}

	if api.fullCalls != 1 {
		t.Fatalf("unnecessary full channel requests: %d", api.fullCalls)
	}
}

func TestResolveDiscussionPreservesErrors(t *testing.T) {
	for _, failure := range []error{context.Canceled, errors.New("network unavailable"), tgerr.New(420, "FLOOD_WAIT_30")} {
		t.Run(failure.Error(), func(t *testing.T) {
			service, api, progress := newDiscussionTestService(t)
			api.fullErr = failure

			_, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID))
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v, want %v", err, failure)
			}

			if len(progress.failed) != 1 {
				t.Fatalf("progress = %+v", progress)
			}
		})
	}
}

func TestResolveDiscussionDoesNotScanOnNetworkFailure(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)
	api.resolveErr = errors.New("network unavailable")

	_, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID))
	if !errors.Is(err, api.resolveErr) || api.dialogCalls != 0 || api.fullCalls != 0 {
		t.Fatalf("error=%v, dialogs=%d full=%d", err, api.dialogCalls, api.fullCalls)
	}
}

func TestResolveDiscussionNotFound(t *testing.T) {
	service, api, progress := newDiscussionTestService(t)

	_, err := service.ResolveTDLibID(context.Background(), channelTDLibID(12345))
	var notFound *peers.PeerNotFoundError
	if !errors.As(err, &notFound) || len(progress.failed) != 1 {
		t.Fatalf("error=%v, progress=%+v", err, progress)
	}

	if api.fullCalls != 1 {
		t.Fatalf("full channel calls=%d", api.fullCalls)
	}
}

func TestResolveDiscussionRejectsIncompleteTarget(t *testing.T) {
	for _, minimal := range []bool{true, false} {
		t.Run(fmt.Sprint("minimal=", minimal), func(t *testing.T) {
			service, api, _ := newDiscussionTestService(t)
			api.discussion.Min = minimal
			if !minimal {
				api.discussion.AccessHash = 0
			}

			_, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID))
			var notFound *peers.PeerNotFoundError
			if !errors.As(err, &notFound) {
				t.Fatalf("expected missing peer, got %v", err)
			}
		})
	}
}

func TestResolveDiscussionSkipsChannelsWithoutDiscussion(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)
	api.parent.HasLink = false

	_, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID))
	var notFound *peers.PeerNotFoundError
	if !errors.As(err, &notFound) || api.fullCalls != 0 {
		t.Fatalf("error=%v, full channel calls=%d", err, api.fullCalls)
	}
}

func TestResolveDiscussionFindsTargetInDialogs(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)

	peer, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.parent.ID))
	if err != nil {
		t.Fatal(err)
	}

	if peer.ID() != api.parent.ID || api.fullCalls != 0 {
		t.Fatalf("peer=%d, full channel calls=%d", peer.ID(), api.fullCalls)
	}
}

func TestResolveDiscussionPreservesConcurrentUpdate(t *testing.T) {
	service, api, _ := newDiscussionTestService(t)
	service.client.storage = newCoherentPeerStorage(service.client.storage)
	api.beforeFull = func() {
		updated := *api.discussion
		updated.Title = "New title from update"
		updated.AccessHash = 987

		var stored storage.Peer
		stored.FromChat(&updated)
		if err := service.client.storage.Add(context.Background(), stored); err != nil {
			t.Fatal(err)
		}
	}

	peer, err := service.ResolveTDLibID(context.Background(), channelTDLibID(api.discussion.ID))
	if err != nil {
		t.Fatal(err)
	}

	input := peer.InputPeer().(*tg.InputPeerChannel)
	if input.AccessHash != 987 || peer.VisibleName() != "New title from update" {
		t.Fatalf("concurrent update overwritten: hash=%d title=%q", input.AccessHash, peer.VisibleName())
	}
}

func newDiscussionTestService(t *testing.T) (*peerService, *discussionTestInvoker, *recordingProgress) {
	t.Helper()

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1},
	})
	api := &discussionTestInvoker{
		parent: &tg.Channel{
			ID: 1924593081, AccessHash: 123, Title: "Parent", Broadcast: true, HasLink: true,
			Photo: &tg.ChatPhotoEmpty{},
		},
		discussion: &tg.Channel{
			ID: 3716475718, AccessHash: 456, Title: "Comments", Megagroup: true, Left: true,
			Photo: &tg.ChatPhotoEmpty{},
		},
	}
	progress := &recordingProgress{}
	service.client.peerMgr = peers.Options{}.Build(tg.NewClient(api))
	service.client.peerApplier = service.client.peerMgr
	service.client.progress = progress

	return service, api, progress
}

// Only read RPCs are accepted; joining a group or reading message history fails.
type discussionTestInvoker struct {
	parent, discussion     *tg.Channel
	resolveErr, fullErr    error
	dialogCalls, fullCalls int
	includeUnrelated       bool
	beforeFull             func()
}

func (i *discussionTestInvoker) Invoke(ctx context.Context, request bin.Encoder, result bin.Decoder) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch request := request.(type) {
	case *tg.ChannelsGetChannelsRequest:
		if i.resolveErr != nil {
			return i.resolveErr
		}

		return tgerr.New(400, "CHANNEL_INVALID")
	case *tg.MessagesGetDialogsRequest:
		i.dialogCalls++
		response := &tg.MessagesDialogs{
			Dialogs: []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: i.parent.ID}}},
			Chats:   []tg.ChatClass{i.parent},
		}
		if i.includeUnrelated {
			unrelated := *i.parent
			unrelated.ID = 999
			response.Dialogs = append([]tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: unrelated.ID}}}, response.Dialogs...)
			response.Chats = append(response.Chats, &unrelated)

			replies := tg.MessageReplies{}
			replies.SetChannelID(i.discussion.ID)
			message := &tg.Message{ID: 100, PeerID: &tg.PeerChannel{ChannelID: i.parent.ID}}
			message.SetReplies(replies)
			response.Messages = []tg.MessageClass{message}
		}
		result.(*tg.MessagesDialogsBox).Dialogs = response

		return nil
	case *tg.ChannelsGetFullChannelRequest:
		i.fullCalls++
		if i.beforeFull != nil {
			i.beforeFull()
		}

		input := request.Channel.(*tg.InputChannel)
		if input.ChannelID != i.parent.ID || input.AccessHash != i.parent.AccessHash {
			return fmt.Errorf("unexpected parent: %v", input)
		}

		if i.fullErr != nil {
			return i.fullErr
		}

		full := &tg.ChannelFull{ID: i.parent.ID}
		full.SetLinkedChatID(i.discussion.ID)
		*result.(*tg.MessagesChatFull) = tg.MessagesChatFull{
			FullChat: full, Chats: []tg.ChatClass{i.parent, i.discussion},
		}

		return nil
	default:
		return fmt.Errorf("unexpected RPC %T", request)
	}
}
