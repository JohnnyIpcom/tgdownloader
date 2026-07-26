package renderer

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"
)

type ProgressUnit uint8

const (
	ProgressUnitCount ProgressUnit = iota
	ProgressUnitBytes
)

// Event is the transport payload used by interactive renderers.
type Event struct {
	Kind           EventKind
	ID, Text       string
	Label          string
	Current, Total int64
	Unit           ProgressUnit
	Elapsed        time.Duration
}

type EventKind string

const (
	EventLine           EventKind = "line"
	EventProgressCreate EventKind = "progress_create"
	EventProgressUpdate EventKind = "progress_update"
	EventProgressDone   EventKind = "progress_done"
	EventProgressFail   EventKind = "progress_fail"
	EventBarrier        EventKind = "barrier"
)

// EventSink receives renderer events.
type EventSink interface {
	Emit(Event)
}

type discardEventSink struct{}

func (discardEventSink) Emit(Event) {}

// DiscardEvents returns a sink for callers that do not yet consume events.
func DiscardEvents() EventSink {
	return discardEventSink{}
}

type eventSinkContextKey struct{}

// WithEventSink associates renderer output with one command context.
func WithEventSink(ctx context.Context, sink EventSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		sink = DiscardEvents()
	}
	return context.WithValue(ctx, eventSinkContextKey{}, sink)
}

// EventSinkFromContext returns the scoped sink or a no-op sink when absent.
func EventSinkFromContext(ctx context.Context) EventSink {
	if ctx != nil {
		if sink, ok := ctx.Value(eventSinkContextKey{}).(EventSink); ok && sink != nil {
			return sink
		}
	}
	return DiscardEvents()
}

func eventSinkFromContext(ctx context.Context) (EventSink, bool) {
	if ctx == nil {
		return nil, false
	}
	sink, ok := ctx.Value(eventSinkContextKey{}).(EventSink)
	return sink, ok && sink != nil
}

// HasEventSink reports whether renderer events are scoped to this context.
func HasEventSink(ctx context.Context) bool {
	_, ok := eventSinkFromContext(ctx)
	return ok
}

type channelEventSink struct {
	mu       sync.Mutex
	lifetime context.Context
	events   chan<- Event
}

func (s *channelEventSink) Emit(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.events == nil {
		return
	}
	if s.lifetime == nil {
		s.lifetime = context.Background()
	}
	if s.lifetime.Err() != nil {
		return
	}
	select {
	case s.events <- event:
	case <-s.lifetime.Done():
	}
}

// NewChannelEventSink forwards events to a caller-owned channel.
func NewChannelEventSink(events chan<- Event) EventSink {
	return NewContextChannelEventSink(context.Background(), events)
}

// NewContextChannelEventSink forwards events until the supplied lifetime ends.
func NewContextChannelEventSink(lifetime context.Context, events chan<- Event) EventSink {
	return &channelEventSink{lifetime: lifetime, events: events}
}

// EventWriter converts complete newline-delimited writes into transcript events.
type EventWriter struct {
	mu      sync.Mutex
	sink    EventSink
	pending string
}

var _ io.Writer = (*EventWriter)(nil)

func NewEventWriter(sink EventSink) *EventWriter {
	if sink == nil {
		sink = DiscardEvents()
	}
	return &EventWriter{sink: sink}
}

func (w *EventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending += string(p)
	for {
		newline := strings.IndexByte(w.pending, '\n')
		if newline < 0 {
			break
		}
		line := strings.TrimSuffix(w.pending[:newline], "\r")
		w.pending = w.pending[newline+1:]
		w.sink.Emit(Event{Kind: EventLine, Text: line})
	}
	return len(p), nil
}

// Flush emits a pending final fragment that was not newline-terminated.
func (w *EventWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pending == "" {
		return
	}
	line := strings.TrimSuffix(w.pending, "\r")
	w.pending = ""
	w.sink.Emit(Event{Kind: EventLine, Text: line})
}
