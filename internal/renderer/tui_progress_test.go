package renderer

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestNewProgressForContextUsesScopedEventSink(t *testing.T) {
	sink := &recordingSink{}
	progress := NewProgressForContext(WithEventSink(context.Background(), sink))

	if _, ok := progress.(*tuiProgress); !ok {
		t.Fatalf("progress type = %T, want *tuiProgress", progress)
	}
	progress.UnitsTracker("work", 1).Done()
	if got := len(sink.Events()); got != 2 {
		t.Fatalf("event count = %d, want create and done", got)
	}
}

func TestTUITrackerUpdatesOneStableID(t *testing.T) {
	sink := &recordingSink{}
	progress := NewTUIProgress(sink)
	tracker := progress.UnitsTracker("Scanning history", 100)
	tracker.(*tuiTracker).startedAt = time.Now().Add(-time.Second)
	tracker.Increment(40)
	tracker.UpdateMessage("Scanning messages")
	tracker.Done()

	events := sink.Events()
	if len(events) != 4 {
		t.Fatalf("events = %+v, want create, two updates, and done", events)
	}
	for _, event := range events {
		if event.ID == "" || event.ID != events[0].ID {
			t.Fatalf("unstable event IDs: %+v", events)
		}
	}
	wantKinds := []EventKind{EventProgressCreate, EventProgressUpdate, EventProgressUpdate, EventProgressDone}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("event %d kind = %q, want %q", i, events[i].Kind, want)
		}
	}
	if events[1].Current != 40 || events[1].Total != 100 {
		t.Fatalf("increment event = %+v", events[1])
	}
	for _, event := range events {
		if event.Text != "" {
			t.Fatalf("progress event text = %q, want raw structured fields only", event.Text)
		}
		if event.Label != "Scanning history" && event.Label != "Scanning messages" {
			t.Fatalf("event label = %q", event.Label)
		}
		if event.Unit != ProgressUnitCount {
			t.Fatalf("event unit = %v, want count", event.Unit)
		}
	}
	if events[len(events)-1].Elapsed < time.Second {
		t.Fatalf("terminal elapsed = %v, want at least one second", events[len(events)-1].Elapsed)
	}
}

func TestTUIWaitAndStopReturnsWithLiveStatusContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sink := &recordingSink{}
	progress := NewTUIProgress(sink)
	progress.EnablePS(ctx)
	work := progress.UnitsTracker("work", 1)

	done := make(chan struct{})
	go func() {
		progress.WaitAndStop(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("WaitAndStop returned before real work completed")
	case <-time.After(20 * time.Millisecond):
	}

	work.Increment(1)
	work.Done()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitAndStop did not return after real work completed")
	}

	events := sink.Events()
	if len(events) < 3 || events[0].Kind != EventProgressCreate {
		t.Fatalf("events = %+v, want status create before work events", events)
	}
	statusID := events[0].ID
	terminalCount := 0
	for _, event := range events {
		if event.ID == statusID && (event.Kind == EventProgressDone || event.Kind == EventProgressFail) {
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("status terminal event count = %d, want 1; events = %+v", terminalCount, events)
	}
}

func TestTUITrackerEmitsTerminalEventOnce(t *testing.T) {
	sink := &recordingSink{}
	tracker := NewTUIProgress(sink).UnitsTracker("work", 1)

	tracker.Done()
	tracker.Done()
	tracker.Fail()
	tracker.Increment(1)

	events := sink.Events()
	if len(events) != 2 || events[1].Kind != EventProgressDone {
		t.Fatalf("events = %+v, want one create and one done", events)
	}
}

func TestTUITrackerConcurrentCallsAreRaceSafe(t *testing.T) {
	sink := &recordingSink{}
	tracker := NewTUIProgress(sink).UnitsTracker("work", 100)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Increment(1)
			tracker.UpdateMessage("work")
		}()
	}
	wg.Wait()
	tracker.Fail()

	events := sink.Events()
	if got := events[len(events)-1]; got.Kind != EventProgressFail || got.Current != 100 {
		t.Fatalf("terminal event = %+v", got)
	}
}

func TestTUIBytesTrackerWritesAndTracksBytes(t *testing.T) {
	sink := &recordingSink{}
	tracker := NewTUIProgress(sink).BytesTracker(io.Discard, "file.bin", 4)

	n, err := tracker.Write([]byte("data"))
	if err != nil || n != 4 {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	tracker.Done()

	events := sink.Events()
	if got := events[len(events)-1]; got.Current != 4 || got.Total != 4 || got.Label != "file.bin" {
		t.Fatalf("terminal event = %+v", got)
	}
	for _, event := range events {
		if event.Unit != ProgressUnitBytes {
			t.Fatalf("event unit = %v, want bytes", event.Unit)
		}
	}
}
