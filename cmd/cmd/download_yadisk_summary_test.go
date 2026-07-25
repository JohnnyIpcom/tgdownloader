package cmd

import (
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/yadisk"
)

func TestYandexDownloadSummaryAddLinkResult(t *testing.T) {
	t.Parallel()

	s := yandexDownloadSummary{}
	s.AddLinkResult(3, 2, nil)
	s.AddLinkResult(1, 0, errors.New("boom"))

	downloaded, skipped, failed := s.Values()
	if downloaded != 4 || skipped != 2 || failed != 1 {
		t.Fatalf("unexpected summary values: downloaded=%d skipped=%d failed=%d", downloaded, skipped, failed)
	}
}

func TestYandexDownloadSummaryMarkFailed(t *testing.T) {
	t.Parallel()

	s := yandexDownloadSummary{}
	s.MarkFailed()
	s.MarkFailed()

	downloaded, skipped, failed := s.Values()
	if downloaded != 0 || skipped != 0 || failed != 2 {
		t.Fatalf("unexpected summary values: downloaded=%d skipped=%d failed=%d", downloaded, skipped, failed)
	}
}

type yandexTrackerStub struct {
	done bool
}

func (t *yandexTrackerStub) Write(p []byte) (int, error) { return len(p), nil }
func (t *yandexTrackerStub) Increment(int64)             {}
func (t *yandexTrackerStub) UpdateMessage(string)        {}
func (t *yandexTrackerStub) Fail()                       {}
func (t *yandexTrackerStub) Done()                       { t.done = true }

func TestTerminalizeYandexSkippedFileCompletesOrdinaryTracker(t *testing.T) {
	tracker := &yandexTrackerStub{}
	if skipped := terminalizeYandexFileTracker(nil, tracker); !skipped {
		t.Fatal("nil downloaded file was not classified as skipped")
	}
	if !tracker.done {
		t.Fatal("ordinary byte tracker was not terminalized")
	}
}

type yandexEventSink struct {
	mu     sync.Mutex
	events []renderer.Event
}

func (s *yandexEventSink) Emit(event renderer.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *yandexEventSink) Events() []renderer.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]renderer.Event(nil), s.events...)
}

func TestTerminalizeYandexSkippedFileCompletesTUITracker(t *testing.T) {
	sink := &yandexEventSink{}
	tracker := renderer.NewTUIProgress(sink).BytesTracker(io.Discard, "skipped.bin", 10)
	if skipped := terminalizeYandexFileTracker((*yadisk.DownloadedFile)(nil), tracker); !skipped {
		t.Fatal("nil downloaded file was not classified as skipped")
	}

	events := sink.Events()
	if got := events[len(events)-1].Kind; got != renderer.EventProgressDone {
		t.Fatalf("terminal event = %q, want %q", got, renderer.EventProgressDone)
	}
}
