package telegram

import (
	"context"
	"reflect"
	"testing"

	contribbbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/tg"
)

func TestDialogPeerNameUsesVisibleNameForUser(t *testing.T) {
	peer := DialogPeer{Peer: storage.Peer{
		User: &tg.User{
			Username:  "johnny",
			FirstName: "John",
			LastName:  "Doe",
		},
	}}

	if got := peer.Name(); got != "John Doe" {
		t.Fatalf("expected visible name, got %q", got)
	}
	if got := peer.SearchNames(); !reflect.DeepEqual(got, []string{"John Doe", "johnny"}) {
		t.Fatalf("expected visible name and username aliases, got %q", got)
	}
}

func TestDialogPeerNameUsesVisibleNameForUserWithoutUsername(t *testing.T) {
	peer := DialogPeer{Peer: storage.Peer{
		User: &tg.User{
			FirstName: "John",
			LastName:  "Doe",
		},
	}}

	if got := peer.Name(); got != "John Doe" {
		t.Fatalf("expected visible name, got %q", got)
	}
}

func TestDialogPeerNameUsesFallbackForUnnamedUser(t *testing.T) {
	tests := []struct {
		name string
		user *tg.User
		want string
	}{
		{
			name: "Deleted",
			user: &tg.User{ID: 3, Deleted: true},
			want: "<deleted user>",
		},
		{
			name: "Unnamed",
			user: &tg.User{ID: 4},
			want: "<user 4>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := DialogPeer{Peer: storage.Peer{User: tt.user}}
			if got := peer.Name(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDialogCacheLoadsOnlyDialogIndex(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}

	dialogPeer := testStoredUser(1, "Dialog")
	nonDialogPeer := testStoredUser(2, "Participant")
	if err := store.replace(context.Background(), []storage.Peer{dialogPeer}); err != nil {
		t.Fatal(err)
	}
	if err := peerStorage.Add(context.Background(), nonDialogPeer); err != nil {
		t.Fatal(err)
	}

	service, err := newDialogCache(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.GetDialogPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key.ID != dialogPeer.Key.ID {
		t.Fatalf("expected only indexed dialog peer, got %+v", got)
	}
}

func TestDialogCacheRejectsNilContext(t *testing.T) {
	db := openDialogCacheStoreTestDB(t)
	peerStorage := contribbbolt.NewPeerStorage(db, []byte("peers"))
	store, err := newDialogCacheStore(db, peerStorage)
	if err != nil {
		t.Fatal(err)
	}
	service, err := newDialogCache(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.GetDialogPeers(nil); err == nil {
		t.Fatal("expected nil context error")
	}
}
