package renderer

import (
	"strings"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

func TestRenderPeerTableEmitsSortedStructuredRows(t *testing.T) {
	var manager peers.Manager
	items := []peers.Peer{
		manager.User(&tg.User{ID: 2, FirstName: "Zed"}),
		manager.User(&tg.User{ID: 1, FirstName: "Anastasiia"}),
	}
	sink := &recordingSink{}

	RenderPeerTable(NewEventWriter(sink), items)

	events := sink.Events()
	if len(events) != 1 || events[0].Table == nil {
		t.Fatalf("events = %+v, want structured table", events)
	}
	rows := events[0].Table.Rows
	if len(rows) != 2 || !strings.Contains(strings.Join(rows[0], " "), "Anastasiia") {
		t.Fatalf("rows are not sorted by visible name: %q", rows)
	}
}

func TestRenderUserEmitsStructuredTable(t *testing.T) {
	var manager peers.Manager
	user := manager.User(&tg.User{ID: 1, FirstName: "Anastasiia"})
	sink := &recordingSink{}

	RenderUser(NewEventWriter(sink), user)

	events := sink.Events()
	if len(events) != 1 || events[0].Table == nil || len(events[0].Table.Rows) != 1 {
		t.Fatalf("events = %+v, want one structured user table", events)
	}
}
