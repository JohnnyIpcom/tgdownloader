package renderer

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestBytesTrackerWriteWithoutWriter(t *testing.T) {
	t.Parallel()

	progress := NewProgress()
	tracker := progress.BytesTracker(nil, "test", 4)
	defer progress.Stop()

	n, err := tracker.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() = %d, want 4", n)
	}
	tracker.Done()
}

func TestTerminalProgressSinkShowsUnknownProgressBeforeCompletion(t *testing.T) {
	var output bytes.Buffer
	sink := &terminalProgressSink{writer: &output, terminal: true}

	sink.Emit(Event{Kind: EventProgressCreate, ID: "setup", Label: "Prompt setup"})
	sink.mu.Lock()
	got := output.String()
	sink.mu.Unlock()
	if !strings.Contains(got, "Prompt setup") || !strings.Contains(got, "[") {
		t.Fatalf("active progress not rendered: %q", got)
	}
	sink.Emit(Event{Kind: EventProgressDone, ID: "setup", Label: "Prompt setup"})
}

func TestTerminalProgressSinkAnimatesUnknownProgress(t *testing.T) {
	var output bytes.Buffer
	sink := &terminalProgressSink{writer: &output, terminal: true}
	sink.Emit(Event{Kind: EventProgressCreate, ID: "setup", Label: "Prompt setup"})
	sink.mu.Lock()
	initial := output.String()
	sink.mu.Unlock()

	time.Sleep(160 * time.Millisecond)
	sink.mu.Lock()
	got := output.String()
	sink.mu.Unlock()
	if got == initial {
		t.Fatalf("unknown progress remained static: %q", got)
	}
	lastFrame := ansi.Strip(got[strings.LastIndex(got, "\r"):])
	if strings.Contains(lastFrame, "[0s]") {
		t.Fatalf("elapsed time remained static: %q", lastFrame)
	}
	sink.Emit(Event{Kind: EventProgressDone, ID: "setup", Label: "Prompt setup"})
}
