package telegram

import (
	"context"
	"testing"
	"time"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type recordingPeerApplier struct {
	users []tg.UserClass
	chats []tg.ChatClass
	err   error
}

func (a *recordingPeerApplier) Apply(_ context.Context, users []tg.UserClass, chats []tg.ChatClass) error {
	a.users = append([]tg.UserClass(nil), users...)
	a.chats = append([]tg.ChatClass(nil), chats...)

	return a.err
}

func TestApplyStoredPeersFeedsAllCompleteEntities(t *testing.T) {
	applier := &recordingPeerApplier{}
	client := &Client{peerApplier: applier}

	user := testStoredUser(1, "User")
	user.Key.AccessHash = 11

	channel := testStoredChannel(3, "Channel")
	channel.Key.AccessHash = 33

	stored := []storage.Peer{
		user,
		testStoredChat(2, "Chat"),
		channel,
		{Version: storage.LatestVersion},
	}

	if err := client.applyStoredPeers(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	if len(applier.users) != 1 || len(applier.chats) != 2 {
		t.Fatalf("applied users=%d chats=%d", len(applier.users), len(applier.chats))
	}
}

func TestApplyStoredPeersUsesCanonicalHashesAndSkipsIncompleteEntities(t *testing.T) {
	managerStorage := &peers.InmemoryStorage{}
	manager := peers.Options{Storage: managerStorage}.Build(nil)
	client := &Client{peerApplier: manager}

	stored := []storage.Peer{
		{
			Version: storage.LatestVersion,
			Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 1, AccessHash: 11},
			User:    &tg.User{ID: 1, FirstName: "User"},
		},
		{
			Version: storage.LatestVersion,
			Key:     dialogs.DialogKey{Kind: dialogs.Channel, ID: 2, AccessHash: 22},
			Channel: &tg.Channel{ID: 2, Title: "Channel", Photo: &tg.ChatPhotoEmpty{}},
		},
		{
			Version: storage.LatestVersion,
			Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 3, AccessHash: 33},
			User:    &tg.User{ID: 3, AccessHash: 33, Min: true},
		},
	}

	if err := client.applyStoredPeers(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	user, found, err := managerStorage.Find(context.Background(), peers.Key{Prefix: "users_", ID: 1})
	if err != nil || !found || user.AccessHash != 11 {
		t.Fatalf("user found=%v hash=%d err=%v", found, user.AccessHash, err)
	}

	channel, found, err := managerStorage.Find(context.Background(), peers.Key{Prefix: "channel_", ID: 2})
	if err != nil || !found || channel.AccessHash != 22 {
		t.Fatalf("channel found=%v hash=%d err=%v", found, channel.AccessHash, err)
	}

	if _, found, err := managerStorage.Find(context.Background(), peers.Key{Prefix: "users_", ID: 3}); err != nil || found {
		t.Fatalf("minimal user found=%v err=%v", found, err)
	}
}

func TestSyncPeerManagerUsesDialogCacheSnapshot(t *testing.T) {
	applier := &recordingPeerApplier{}

	stored := testStoredUser(7, "Cached")
	stored.Key.AccessHash = 77

	cache := &dialogCache{
		peers: map[storage.PeerKey]DialogPeer{
			storage.KeyFromPeer(stored): {Peer: stored},
		},
	}
	client := &Client{peerApplier: applier, dialogCache: cache}

	if err := client.syncPeerManager(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(applier.users) != 1 || applier.users[0].GetID() != 7 {
		t.Fatalf("applied users = %#v", applier.users)
	}
}

func TestSyncPeerManagerReloadsCanonicalStorage(t *testing.T) {
	ctx := context.Background()
	db := openDialogCacheStoreTestDB(t)
	peerStorage := newCoherentPeerStorage(contribbbolt.NewPeerStorage(db, []byte("peers")))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	old := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: time.Now().Add(-time.Second),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 7, AccessHash: 11},
		User:      &tg.User{ID: 7, AccessHash: 11, FirstName: "Old"},
	}

	if err := store.replace(ctx, []storage.Peer{old}); err != nil {
		t.Fatal(err)
	}

	cache, err := newDialogCache(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	newer := old
	newer.CreatedAt = time.Now()
	newer.Key.AccessHash = 22
	newer.User = &tg.User{ID: 7, AccessHash: 22, FirstName: "New"}

	if err := peerStorage.Add(ctx, newer); err != nil {
		t.Fatal(err)
	}

	managerStorage := &peers.InmemoryStorage{}
	manager := peers.Options{Storage: managerStorage}.Build(nil)
	client := &Client{
		peerApplier: manager,
		dialogCache: cache,
		peerSync:    &peerSyncCoordinator{},
	}

	if err := client.syncPeerManager(ctx); err != nil {
		t.Fatal(err)
	}

	got, found, err := managerStorage.Find(ctx, peers.Key{Prefix: "users_", ID: 7})
	if err != nil || !found || got.AccessHash != 22 {
		t.Fatalf("manager peer found=%v hash=%d err=%v", found, got.AccessHash, err)
	}
}

func TestDialogRefreshAppliesCanonicalPeersToManagerAndCache(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	store, err := newDialogCacheStore(db, contribbbolt.NewPeerStorage(db, []byte("peers")))
	if err != nil {
		t.Fatal(err)
	}

	cache, err := newDialogCache(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}

	applier := &recordingPeerApplier{}
	client := &Client{peerApplier: applier, dialogCache: cache}
	service := &dialogService{client: client}

	stored := []storage.Peer{testStoredUser(9, "Refreshed")}
	stored[0].Key.AccessHash = 99

	if err := service.commitDialogRefresh(context.Background(), stored, cache.startRefresh()); err != nil {
		t.Fatal(err)
	}

	if len(applier.users) != 1 || applier.users[0].GetID() != 9 {
		t.Fatalf("applied users = %#v", applier.users)
	}

	peers, err := cache.GetDialogPeers(context.Background())
	if err != nil || len(peers) != 1 || peers[0].Name() != "Refreshed" {
		t.Fatalf("cached peers=%#v err=%v", peers, err)
	}
}

func TestDialogRefreshKeepsNewerCanonicalPeer(t *testing.T) {
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

	older := storage.Peer{
		Version:   storage.LatestVersion,
		CreatedAt: time.Now().Truncate(time.Second),
		Key:       dialogs.DialogKey{Kind: dialogs.User, ID: 12, AccessHash: 11},
		User:      &tg.User{ID: 12, AccessHash: 11, FirstName: "Old"},
	}

	if err := cache.ReplaceDialogs(ctx, []storage.Peer{older}); err != nil {
		t.Fatal(err)
	}

	refreshToken := cache.startRefresh()

	managerStorage := &peers.InmemoryStorage{}
	manager := peers.Options{Storage: managerStorage}.Build(nil)
	client := &Client{
		peerApplier: manager,
		dialogCache: cache,
		peerSync:    &peerSyncCoordinator{},
	}

	service := &dialogService{client: client}

	newer := older
	newer.Key.AccessHash = 22
	newer.User = &tg.User{ID: 12, AccessHash: 22, FirstName: "New"}

	if err := cache.UpsertDialog(ctx, newer); err != nil {
		t.Fatal(err)
	}

	if err := service.commitDialogRefresh(ctx, []storage.Peer{older}, refreshToken); err != nil {
		t.Fatal(err)
	}

	peersInCache, err := cache.GetDialogPeers(ctx)
	if err != nil || len(peersInCache) != 1 || peersInCache[0].Name() != "New" {
		t.Fatalf("cached peers=%#v err=%v", peersInCache, err)
	}

	got, found, err := managerStorage.Find(ctx, peers.Key{Prefix: "users_", ID: 12})
	if err != nil || !found || got.AccessHash != 22 {
		t.Fatalf("manager peer found=%v hash=%d err=%v", found, got.AccessHash, err)
	}
}

func TestDialogRefreshPreservesConcurrentMembershipChanges(t *testing.T) {
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

	first := testStoredUser(1, "First")
	first.Key.AccessHash = 11

	removed := testStoredUser(2, "Removed")
	removed.Key.AccessHash = 22

	if err := cache.ReplaceDialogs(ctx, []storage.Peer{first, removed}); err != nil {
		t.Fatal(err)
	}

	refreshToken := cache.startRefresh()

	// Simulate membership changes arriving while the refresh is paginating.
	added := testStoredUser(3, "Added")
	added.Key.AccessHash = 33

	if err := cache.UpsertDialog(ctx, added); err != nil {
		t.Fatal(err)
	}

	if err := cache.RemoveDialog(ctx, storage.KeyFromPeer(removed)); err != nil {
		t.Fatal(err)
	}

	client := &Client{
		peerApplier: &recordingPeerApplier{},
		dialogCache: cache,
		peerSync:    &peerSyncCoordinator{},
	}

	service := &dialogService{client: client}

	if err := service.commitDialogRefresh(ctx, []storage.Peer{first, removed}, refreshToken); err != nil {
		t.Fatal(err)
	}

	peersInCache, err := cache.GetDialogPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(peersInCache) != 2 || peersInCache[0].Name() != "Added" || peersInCache[1].Name() != "First" {
		t.Fatalf("cached peers=%#v", peersInCache)
	}
}

func TestDialogRefreshPrefersCompleteEntityOverConcurrentMinimal(t *testing.T) {
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

	refreshToken := cache.startRefresh()

	minimal := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 13},
		User:    &tg.User{ID: 13, Min: true, FirstName: "Min"},
	}

	if err := cache.UpsertDialog(ctx, minimal); err != nil {
		t.Fatal(err)
	}

	full := storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.User, ID: 13, AccessHash: 13},
		User:    &tg.User{ID: 13, AccessHash: 13, FirstName: "Full"},
	}

	client := &Client{
		peerApplier: &recordingPeerApplier{},
		dialogCache: cache,
		peerSync:    &peerSyncCoordinator{},
	}

	service := &dialogService{client: client}

	if err := service.commitDialogRefresh(ctx, []storage.Peer{full}, refreshToken); err != nil {
		t.Fatal(err)
	}

	peersInCache, err := cache.GetDialogPeers(ctx)
	if err != nil || len(peersInCache) != 1 || peersInCache[0].Name() != "Full" {
		t.Fatalf("cached peers=%#v err=%v", peersInCache, err)
	}
}
