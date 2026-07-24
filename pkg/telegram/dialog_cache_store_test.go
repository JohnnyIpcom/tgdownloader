package telegram

import (
	"context"
	"testing"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	bolt "go.etcd.io/bbolt"
)

func openDialogCacheStoreTestDB(t *testing.T) *bolt.DB {
	t.Helper()

	db, err := bolt.Open(t.TempDir()+"/storage.db", 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func testStoredUser(id int64, firstName string) storage.Peer {
	return storage.Peer{
		Version: storage.LatestVersion,
		Key: dialogs.DialogKey{
			Kind: dialogs.User,
			ID:   id,
		},
		User: &tg.User{ID: id, FirstName: firstName},
	}
}

func testStoredChat(id int64, title string) storage.Peer {
	return storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.Chat, ID: id},
		Chat:    &tg.Chat{ID: id, Title: title, Photo: &tg.ChatPhotoEmpty{}},
	}
}

func testStoredChannel(id int64, title string) storage.Peer {
	return storage.Peer{
		Version: storage.LatestVersion,
		Key:     dialogs.DialogKey{Kind: dialogs.Channel, ID: id},
		Channel: &tg.Channel{ID: id, Title: title, Photo: &tg.ChatPhotoEmpty{}},
	}
}

func TestDialogCacheStoreResetsLegacyCache(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("peers"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte("legacy"), []byte("value"))
	}); err != nil {
		t.Fatal(err)
	}

	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	peers, err := store.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected empty cache after migration, got %d peers", len(peers))
	}

	if err := db.View(func(tx *bolt.Tx) error {
		if got := tx.Bucket([]byte("peers")).Get([]byte("legacy")); got != nil {
			t.Fatalf("expected legacy peer data to be removed, got %q", got)
		}
		if tx.Bucket(dialogCacheBucket) == nil {
			t.Fatal("expected dialogs bucket")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDialogCacheStoreReplaceAndLoad(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	first := testStoredUser(1, "First")
	stale := testStoredUser(2, "Stale")
	if err := store.replace(context.Background(), []storage.Peer{first, stale}); err != nil {
		t.Fatal(err)
	}
	if err := store.replace(context.Background(), []storage.Peer{first}); err != nil {
		t.Fatal(err)
	}

	got, err := store.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key.ID != first.Key.ID {
		t.Fatalf("expected only peer %d, got %+v", first.Key.ID, got)
	}
}

func TestDialogCacheStoreKeepsCurrentSchemaDataOnReopen(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}
	peer := testStoredUser(1, "Persisted")
	if err := store.replace(context.Background(), []storage.Peer{peer}); err != nil {
		t.Fatal(err)
	}

	reopened, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key.ID != peer.Key.ID {
		t.Fatalf("expected persisted dialog after reopen, got %+v", got)
	}
}

func TestDialogCacheStoreSupportsAllPeerKindsAndRemove(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	peers := []storage.Peer{
		testStoredUser(1, "User"),
		testStoredChat(2, "Chat"),
		testStoredChannel(3, "Channel"),
	}
	if err := store.replace(context.Background(), peers); err != nil {
		t.Fatal(err)
	}
	if err := store.remove(context.Background(), storage.KeyFromPeer(peers[1])); err != nil {
		t.Fatal(err)
	}

	got, err := store.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two peers after remove, got %+v", got)
	}
}

func TestDialogCacheStoreLoadRejectsMissingPeer(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	key := storage.PeerKey{Kind: dialogs.User, ID: 42}
	if err := db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(dialogCacheBucket).Put(key.Bytes(nil), nil)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.load(context.Background()); err == nil {
		t.Fatal("expected missing referenced peer error")
	}
}
