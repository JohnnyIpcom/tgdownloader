package telegram

import (
	"context"
	"testing"
	"time"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

func TestCoherentPeerStoragePreservesFullUserFromMinimalUpdate(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	raw := contribbbolt.NewPeerStorage(db, []byte("peers"))
	if _, err := newDialogCacheStore(db, raw); err != nil {
		t.Fatal(err)
	}

	store := newCoherentPeerStorage(raw)

	full := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: time.Now(),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 11},
		User:      &tg.User{ID: 1, AccessHash: 11, FirstName: "Full"},
	}
	minimal := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: full.CreatedAt.Add(time.Second),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1},
		User:      &tg.User{ID: 1, FirstName: "Min", Min: true},
	}

	if err := store.Add(ctx, full); err != nil {
		t.Fatal(err)
	}

	if err := store.Add(ctx, minimal); err != nil {
		t.Fatal(err)
	}

	got, err := store.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	if got.User == nil || got.User.Min || got.User.FirstName != "Full" || got.Key.AccessHash != 11 {
		t.Fatalf("stored peer = %#v", got)
	}
}

func TestCoherentPeerStoragePreservesLiveEntityDuringRefresh(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	raw := contribbbolt.NewPeerStorage(db, []byte("peers"))
	if _, err := newDialogCacheStore(db, raw); err != nil {
		t.Fatal(err)
	}

	store := newCoherentPeerStorage(raw)

	refreshPeer := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: time.Now().Truncate(time.Second),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 11},
		User:      &tg.User{ID: 1, AccessHash: 11, FirstName: "Refresh"},
	}

	if err := store.Add(ctx, refreshPeer); err != nil {
		t.Fatal(err)
	}

	startedRevision := store.Revision()

	livePeer := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: refreshPeer.CreatedAt,
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 22},
		User:      &tg.User{ID: 1, AccessHash: 22, FirstName: "Live"},
	}

	if err := store.Add(ctx, livePeer); err != nil {
		t.Fatal(err)
	}

	if err := store.AddRefresh(ctx, refreshPeer, startedRevision); err != nil {
		t.Fatal(err)
	}

	got, err := store.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	if got.User == nil || got.User.FirstName != "Live" || got.Key.AccessHash != 22 {
		t.Fatalf("stored peer = %#v", got)
	}
}

func TestCoherentPeerStorageDoesNotPromoteMinimalHash(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	raw := contribbbolt.NewPeerStorage(db, []byte("peers"))
	if _, err := newDialogCacheStore(db, raw); err != nil {
		t.Fatal(err)
	}

	store := newCoherentPeerStorage(raw)

	full := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1},
		User:    &tg.User{ID: 1, Self: true, FirstName: "Full"},
	}
	minimal := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 99},
		User:    &tg.User{ID: 1, AccessHash: 99, Min: true},
	}

	if err := store.Add(ctx, full); err != nil {
		t.Fatal(err)
	}

	if err := store.Add(ctx, minimal); err != nil {
		t.Fatal(err)
	}

	got, err := store.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	if got.Key.AccessHash != 0 || got.User == nil || got.User.AccessHash != 0 {
		t.Fatalf("stored peer = %#v", got)
	}
}

func TestCoherentPeerStorageRefreshReplacesNewerMinimalPeer(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	raw := contribbbolt.NewPeerStorage(db, []byte("peers"))
	if _, err := newDialogCacheStore(db, raw); err != nil {
		t.Fatal(err)
	}

	store := newCoherentPeerStorage(raw)
	startedRevision := store.Revision()

	minimal := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1},
		User:    &tg.User{ID: 1, Min: true, FirstName: "Min"},
	}
	full := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 11},
		User:    &tg.User{ID: 1, AccessHash: 11, FirstName: "Full"},
	}

	if err := store.Add(ctx, minimal); err != nil {
		t.Fatal(err)
	}

	if err := store.AddRefresh(ctx, full, startedRevision); err != nil {
		t.Fatal(err)
	}

	got, err := store.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	if got.User == nil || got.User.Min || got.User.FirstName != "Full" || got.Key.AccessHash != 11 {
		t.Fatalf("stored peer = %#v", got)
	}
}

func TestDialogCacheUpsertKeepsFullPeerAfterMinimalUpdate(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	peerStorage := newCoherentPeerStorage(contribbbolt.NewPeerStorage(db, []byte("peers")))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	cache, err := newDialogCache(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	full := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: time.Now(),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 11},
		User:      &tg.User{ID: 1, AccessHash: 11, FirstName: "Full"},
	}
	minimal := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: full.CreatedAt.Add(time.Second),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 1},
		User:      &tg.User{ID: 1, FirstName: "Min", Min: true},
	}

	if err := cache.UpsertDialog(ctx, full); err != nil {
		t.Fatal(err)
	}

	if err := cache.UpsertDialog(ctx, minimal); err != nil {
		t.Fatal(err)
	}

	peers, err := cache.GetDialogPeers(ctx)
	if err != nil || len(peers) != 1 || peers[0].Name() != "Full" {
		t.Fatalf("cached peers=%#v err=%v", peers, err)
	}
}
