package telegram

import (
	"context"
	"errors"
	"testing"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	bolt "go.etcd.io/bbolt"
)

func TestPeerServiceResolveStoredUserPreservesAccessHash(t *testing.T) {
	const (
		id         = int64(707200034)
		accessHash = int64(9123456789)
	)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key: dialogs.DialogKey{
			Kind:       dialogs.User,
			ID:         id,
			AccessHash: accessHash,
		},
		User: &tg.User{ID: id, AccessHash: accessHash, FirstName: "Anastasiia"},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), userTDLibID(id))
	if err != nil || !found {
		t.Fatalf("resolve stored user: found=%v err=%v", found, err)
	}

	input, ok := peer.InputPeer().(*tg.InputPeerUser)
	if !ok || input.UserID != id || input.AccessHash != accessHash {
		t.Fatalf("input peer = %#v", peer.InputPeer())
	}
}

func TestPeerServiceResolveStoredChannelPreservesAccessHash(t *testing.T) {
	const (
		id         = int64(42)
		accessHash = int64(123456789)
	)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key: dialogs.DialogKey{
			Kind:       dialogs.Channel,
			ID:         id,
			AccessHash: accessHash,
		},
		Channel: &tg.Channel{
			ID:         id,
			AccessHash: accessHash,
			Title:      "Channel",
			Photo:      &tg.ChatPhotoEmpty{},
		},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), channelTDLibID(id))
	if err != nil || !found {
		t.Fatalf("resolve stored channel: found=%v err=%v", found, err)
	}

	input, ok := peer.InputPeer().(*tg.InputPeerChannel)
	if !ok || input.ChannelID != id || input.AccessHash != accessHash {
		t.Fatalf("input peer = %#v", peer.InputPeer())
	}
}

func TestPeerServiceResolveStoredPeerRejectsMissingEntity(t *testing.T) {
	const id = int64(7)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: id, AccessHash: 99},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), userTDLibID(id))
	if err != nil || found || peer != nil {
		t.Fatalf("resolve incomplete peer: peer=%v found=%v err=%v", peer, found, err)
	}
}

func TestPeerServiceResolveStoredPeerRejectsMinimalUser(t *testing.T) {
	const id = int64(8)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: id, AccessHash: 99},
		User:    &tg.User{ID: id, AccessHash: 99, Min: true},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), userTDLibID(id))
	if err != nil || found || peer != nil {
		t.Fatalf("resolve minimal user: peer=%v found=%v err=%v", peer, found, err)
	}
}

func TestPeerServiceResolveStoredPeerRejectsMinimalChannel(t *testing.T) {
	const id = int64(9)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.Channel, ID: id, AccessHash: 99},
		Channel: &tg.Channel{
			ID:         id,
			AccessHash: 99,
			Min:        true,
			Photo:      &tg.ChatPhotoEmpty{},
		},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), channelTDLibID(id))
	if err != nil || found || peer != nil {
		t.Fatalf("resolve minimal peer: peer=%v found=%v err=%v", peer, found, err)
	}
}

func TestPeerServiceResolveStoredSelfWithoutAccessHash(t *testing.T) {
	const id = int64(10)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: id},
		User:    &tg.User{ID: id, Self: true, FirstName: "Self"},
	})

	peer, found, err := service.resolveStoredTDLibID(context.Background(), userTDLibID(id))
	if err != nil || !found {
		t.Fatalf("resolve stored self: found=%v err=%v", found, err)
	}

	if _, ok := peer.InputPeer().(*tg.InputPeerSelf); !ok {
		t.Fatalf("input peer = %#v", peer.InputPeer())
	}
}

func TestPeerServiceResolveTDLibIDFallsBackToManager(t *testing.T) {
	const id = int64(11)

	service := newStoredPeerService(t, storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: id},
	})

	fallbackErr := errors.New("manager fallback")
	service.client.peerMgr = peers.Options{}.Build(tg.NewClient(errorInvoker{err: fallbackErr}))

	if _, err := service.ResolveTDLibID(context.Background(), userTDLibID(id)); !errors.Is(err, fallbackErr) {
		t.Fatalf("fallback error = %v", err)
	}
}

type errorInvoker struct {
	err error
}

func (i errorInvoker) Invoke(context.Context, bin.Encoder, bin.Decoder) error {
	return i.err
}

func newStoredPeerService(t *testing.T, stored storage.Peer) *peerService {
	t.Helper()

	db, err := bolt.Open(t.TempDir()+"/storage.db", 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	if err := peerStorage.Add(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	client := &Client{
		peerMgr: peers.Options{}.Build(nil),
		storage: peerStorage,
	}

	return &peerService{client: client}
}

func userTDLibID(id int64) (peerID constant.TDLibPeerID) {
	peerID.User(id)

	return peerID
}

func channelTDLibID(id int64) (peerID constant.TDLibPeerID) {
	peerID.Channel(id)

	return peerID
}
