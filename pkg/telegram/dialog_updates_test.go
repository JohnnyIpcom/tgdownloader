package telegram

import (
	"context"
	"testing"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func newDialogUpdateTestCache(t *testing.T) *dialogCache {
	t.Helper()

	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := newDialogCache(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestDialogUpdateHandlersAddDirectUserDialog(t *testing.T) {
	cache := newDialogUpdateTestCache(t)
	dispatcher := tg.NewUpdateDispatcher()
	registerDialogCacheHandlers(dispatcher, cache, zap.NewNop())

	updates := &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{Message: &tg.Message{PeerID: &tg.PeerUser{UserID: 7}}},
		},
		Users: []tg.UserClass{
			&tg.User{ID: 7, FirstName: "Visible", LastName: "User", Username: "alias"},
		},
	}
	if err := dispatcher.Handle(context.Background(), updates); err != nil {
		t.Fatal(err)
	}

	peers, err := cache.GetDialogPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].Name() != "Visible User" {
		t.Fatalf("expected direct user dialog, got %+v", peers)
	}
}

func TestDialogUpdateHandlersRefreshUserName(t *testing.T) {
	cache := newDialogUpdateTestCache(t)
	if err := cache.UpsertDialog(context.Background(), testStoredUser(7, "Old")); err != nil {
		t.Fatal(err)
	}

	dispatcher := tg.NewUpdateDispatcher()
	registerDialogCacheHandlers(dispatcher, cache, zap.NewNop())
	updates := &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateUserName{
				UserID:    7,
				FirstName: "New",
				LastName:  "Name",
				Usernames: []tg.Username{{Username: "new_alias", Active: true}},
			},
		},
	}
	if err := dispatcher.Handle(context.Background(), updates); err != nil {
		t.Fatal(err)
	}

	peers, err := cache.GetDialogPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].Name() != "New Name" {
		t.Fatalf("expected refreshed visible name, got %+v", peers)
	}
	if peers[0].User.Username != "new_alias" {
		t.Fatalf("expected refreshed username, got %q", peers[0].User.Username)
	}
}
