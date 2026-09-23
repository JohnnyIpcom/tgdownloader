package telegram

import (
	"context"
	"testing"

	contribbbolt "github.com/gotd/contrib/bbolt"
	contribstorage "github.com/gotd/contrib/storage"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type countingUpdateHandler struct {
	calls int
}

func (h *countingUpdateHandler) Handle(context.Context, tg.UpdatesClass) error {
	h.calls++

	return nil
}

func TestDeferredUpdateHandlerReplacesDelegate(t *testing.T) {
	first := &countingUpdateHandler{}
	second := &countingUpdateHandler{}
	handler := newDeferredUpdateHandler(first, nil)

	if err := handler.Handle(context.Background(), &tg.Updates{}); err != nil {
		t.Fatal(err)
	}

	handler.Set(second)

	if err := handler.Handle(context.Background(), &tg.Updates{}); err != nil {
		t.Fatal(err)
	}

	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("handler calls: first=%d second=%d", first.calls, second.calls)
	}
}

func TestPeerManagerAndPersistentStorageReceiveSameUpdate(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	persistent := contribbbolt.NewPeerStorage(db, []byte("peers"))
	next := &countingUpdateHandler{}
	base := contribstorage.UpdateHook(next, persistent)
	handler := newDeferredUpdateHandler(base, nil)

	managerStorage := &peers.InmemoryStorage{}
	manager := peers.Options{Storage: managerStorage}.Build(nil)
	handler.Set(manager.UpdateHook(base))

	const (
		id         = int64(707200034)
		accessHash = int64(9123456789)
	)

	update := &tg.Updates{Users: []tg.UserClass{
		&tg.User{ID: id, AccessHash: accessHash, FirstName: "Anastasiia"},
	}}

	if err := handler.Handle(ctx, update); err != nil {
		t.Fatal(err)
	}

	stored, err := persistent.Find(ctx, contribstorage.PeerKey{Kind: dialogs.User, ID: id})
	if err != nil || stored.Key.AccessHash != accessHash {
		t.Fatalf("persisted peer hash=%d err=%v", stored.Key.AccessHash, err)
	}

	managerValue, found, err := managerStorage.Find(ctx, peers.Key{Prefix: "users_", ID: id})
	if err != nil || !found || managerValue.AccessHash != accessHash {
		t.Fatalf("manager peer found=%v hash=%d err=%v", found, managerValue.AccessHash, err)
	}

	if next.calls != 1 {
		t.Fatalf("downstream calls = %d", next.calls)
	}
}

var _ tgclient.UpdateHandler = (*countingUpdateHandler)(nil)
