package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type dialogCacheStub struct {
	peers []telegram.DialogPeer
	calls int
	ctx   context.Context
}

func (s *dialogCacheStub) GetDialogPeers(ctx context.Context, _ ...telegram.DialogPeerFilter) ([]telegram.DialogPeer, error) {
	s.calls++
	s.ctx = ctx
	return s.peers, nil
}

type dialogServiceStub struct {
	dialogs []telegram.Dialog
	calls   int
	after   func()
}

func (s *dialogServiceStub) GetAllDialogs(context.Context) (<-chan telegram.Dialog, int, error) {
	s.calls++
	out := make(chan telegram.Dialog, len(s.dialogs))
	for _, dialog := range s.dialogs {
		out <- dialog
	}
	close(out)
	if s.after != nil {
		s.after()
	}
	return out, len(s.dialogs), nil
}

func TestDialogListReadsCacheWithoutRefresh(t *testing.T) {
	cache := &dialogCacheStub{peers: []telegram.DialogPeer{cachedChannel(123, "Cherry Channel")}}
	service := &dialogServiceStub{}
	r := &Root{client: &telegram.Client{DialogCache: cache, DialogService: service}}
	cmd := r.newDialogsCmd()
	list, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatal(err)
	}
	list.SetContext(context.Background())

	if err := list.RunE(list, nil); err != nil {
		t.Fatal(err)
	}
	if cache.calls != 1 {
		t.Fatalf("expected one cache read, got %d", cache.calls)
	}
	if service.calls != 0 {
		t.Fatalf("expected no Telegram refresh, got %d", service.calls)
	}
}

func TestDialogListDoesNotRequireConnection(t *testing.T) {
	r := &Root{}
	cmd := r.newDialogsCmd()
	list, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatal(err)
	}
	refresh, _, err := cmd.Find([]string{"refresh"})
	if err != nil {
		t.Fatal(err)
	}

	if list.PreRunE != nil {
		t.Fatal("expected dialog list to run without connecting")
	}
	if refresh.PreRunE == nil {
		t.Fatal("expected dialog refresh to require a connection")
	}
}

func TestDialogRefreshReportsCachedCount(t *testing.T) {
	cache := &dialogCacheStub{peers: []telegram.DialogPeer{
		cachedChannel(123, "Cherry Channel"),
		cachedUser(456, "Anastasiia", "anastasiia"),
	}}
	service := &dialogServiceStub{dialogs: []telegram.Dialog{
		{DialogPeer: cache.peers[0]},
		{DialogPeer: cache.peers[1]},
	}}
	r := &Root{client: &telegram.Client{DialogCache: cache, DialogService: service}}
	cmd := r.newDialogsCmd()
	refresh, _, err := cmd.Find([]string{"refresh"})
	if err != nil {
		t.Fatal(err)
	}
	refresh.SetContext(context.Background())
	var output bytes.Buffer
	refresh.SetOut(&output)

	if err := refresh.RunE(refresh, nil); err != nil {
		t.Fatal(err)
	}
	if service.calls != 1 {
		t.Fatalf("expected one Telegram refresh, got %d", service.calls)
	}
	if cache.calls != 1 {
		t.Fatalf("expected one cache read after refresh, got %d", cache.calls)
	}
	if got, want := output.String(), "Refreshed 2 dialogs\n"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDialogRefreshDoesNotReportSuccessAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &dialogServiceStub{after: cancel}
	r := &Root{client: &telegram.Client{
		DialogCache:   &dialogCacheStub{},
		DialogService: service,
	}}
	cmd := r.newDialogsCmd()
	refresh, _, err := cmd.Find([]string{"refresh"})
	if err != nil {
		t.Fatal(err)
	}
	refresh.SetContext(ctx)
	var output bytes.Buffer
	refresh.SetOut(&output)

	err = refresh.RunE(refresh, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("expected no success output, got %q", output.String())
	}
}
