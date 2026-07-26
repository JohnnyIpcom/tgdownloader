package renderer

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"
)

type recordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordingSink) Emit(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingSink) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

func TestEventSinkRoundTripsThroughContext(t *testing.T) {
	sink := &recordingSink{}
	ctx := WithEventSink(context.Background(), sink)

	EventSinkFromContext(ctx).Emit(Event{Kind: EventLine, Text: "ready"})

	events := sink.Events()
	if len(events) != 1 || events[0].Text != "ready" {
		t.Fatalf("events = %+v, want one ready event", events)
	}
}

func TestEventSinkFromNilContextDiscards(t *testing.T) {
	EventSinkFromContext(nil).Emit(Event{Kind: EventLine, Text: "ignored"})
}

func TestEventWriterEmitsCompleteLines(t *testing.T) {
	sink := &recordingSink{}
	w := NewEventWriter(sink)

	_, _ = io.WriteString(w, "first\nsec")
	_, _ = io.WriteString(w, "ond\r\n")

	if got := eventTexts(sink.Events()); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("lines = %q", got)
	}
}

func TestEventWriterConcurrentWritesAreRaceSafe(t *testing.T) {
	sink := &recordingSink{}
	w := NewEventWriter(sink)
	const writes = 100

	var wg sync.WaitGroup
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.WriteString(w, "line\n")
		}()
	}
	wg.Wait()

	if got := len(sink.Events()); got != writes {
		t.Fatalf("event count = %d, want %d", got, writes)
	}
}

func TestEventWriterFlushesFinalFragment(t *testing.T) {
	sink := &recordingSink{}
	w := NewEventWriter(sink)
	_, _ = io.WriteString(w, "final fragment")
	w.Flush()

	if got := eventTexts(sink.Events()); !reflect.DeepEqual(got, []string{"final fragment"}) {
		t.Fatalf("lines = %q", got)
	}
}

func TestChannelEventSinkDeliversEvent(t *testing.T) {
	events := make(chan Event, 1)
	NewChannelEventSink(events).Emit(Event{Kind: EventLine, Text: "line"})

	if got := <-events; got.Text != "line" {
		t.Fatalf("event = %+v", got)
	}
}

func TestEventCarriesStructuredTable(t *testing.T) {
	table := &TableData{
		Columns: []TableColumn{{Header: "Name", Required: true}},
		Rows:    [][]string{{"dialog"}},
	}
	event := Event{Kind: EventTable, Table: table}

	if event.Table != table || event.Kind != EventTable {
		t.Fatalf("event = %+v, want structured table", event)
	}
}

func TestEventWriterEmitsIndependentStructuredTable(t *testing.T) {
	sink := &recordingSink{}
	writer := NewEventWriter(sink)
	table := TableData{
		Columns: []TableColumn{{Header: "Name", Required: true}},
		Rows:    [][]string{{"dialog"}},
	}

	writer.EmitTable(table)
	table.Columns[0].Header = "Changed"
	table.Rows[0][0] = "changed"

	events := sink.Events()
	if len(events) != 1 || events[0].Kind != EventTable || events[0].Table == nil {
		t.Fatalf("events = %+v, want one table event", events)
	}
	if got := events[0].Table; got.Columns[0].Header != "Name" || got.Rows[0][0] != "dialog" {
		t.Fatalf("event table changed with producer slices: %+v", got)
	}
}

func eventTexts(events []Event) []string {
	texts := make([]string, len(events))
	for i, event := range events {
		texts[i] = event.Text
	}
	return texts
}
