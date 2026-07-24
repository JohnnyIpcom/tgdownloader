package cmd

import (
	"testing"

	"github.com/johnnyipcom/tgdownloader/internal/downloader"
)

type fakeScanTracker struct {
	message    string
	increments int64
	done       bool
}

func (t *fakeScanTracker) Increment(n int64)        { t.increments += n }
func (t *fakeScanTracker) UpdateMessage(msg string) { t.message = msg }
func (t *fakeScanTracker) Fail()                    {}
func (t *fakeScanTracker) Done()                    { t.done = true }

func TestDownloadScanProgressShowsSkippedFiles(t *testing.T) {
	t.Parallel()

	tracker := &fakeScanTracker{}
	progress := newDownloadScanProgress(tracker)

	stats := downloader.Stats{Skipped: 41, Failed: 2}
	progress.FileFound(stats)
	progress.FileFound(stats)
	progress.ScanningDone(stats)

	if tracker.increments != 2 {
		t.Fatalf("increments = %d, want 2", tracker.increments)
	}
	if tracker.message != "Scanning history complete: found=2 skipped=41 failed=2" {
		t.Fatalf("message = %q", tracker.message)
	}
	if tracker.done {
		t.Fatal("tracker completed before download workers stopped")
	}

	progress.Finish(stats)
	if !tracker.done {
		t.Fatal("tracker was not completed")
	}
}
