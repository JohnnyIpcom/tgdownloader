package renderer

import (
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
)

type downloadTracker struct {
	pw progress.Writer
}

var _ downloader.Tracker = (*downloadTracker)(nil)

// DownloadTrackerOption is an option for a download renderer.
type DownloadTrackerOption func(*downloadTracker)

func WithNumTrackersExpected(n int) DownloadTrackerOption {
	return func(dr *downloadTracker) {
		dr.pw.SetNumTrackersExpected(n)
	}
}

// NewDownloadRenderer creates a new download renderer.
func NewDownloadTracker(opts ...DownloadTrackerOption) downloader.Tracker {
	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetMessageWidth(50)
	pw.SetTrackerLength(25)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetSortBy(progress.SortByNone)
	pw.SetStyle(progress.StyleDefault)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.SetNumTrackersExpected(5)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Visibility.ETA = true
	pw.Style().Visibility.ETAOverall = true

	go pw.Render()

	tracker := &downloadTracker{pw: pw}
	for _, opt := range opts {
		opt(tracker)
	}

	return tracker
}

type TrackedWriter interface {
	io.Writer

	Fail()
	Done()
}

type trackedWriter struct {
	tracker *progress.Tracker
	w       io.Writer
}

func (tw *trackedWriter) Write(p []byte) (int, error) {
	n, err := tw.w.Write(p)
	if err != nil {
		tw.tracker.MarkAsErrored()
		return n, err
	}

	tw.tracker.Increment(int64(n))
	return n, nil
}

func (tw *trackedWriter) Fail() {
	tw.tracker.MarkAsErrored()
}

func (tw *trackedWriter) Done() {
	tw.tracker.MarkAsDone()
}

func (dr *downloadTracker) WrapWriter(w io.Writer, msg string, size int64) downloader.TrackedWriter {
	tracker := &progress.Tracker{
		Message: msg,
		Total:   size,
		Units:   progress.UnitsBytes,
	}

	dr.pw.AppendTracker(tracker)
	return &trackedWriter{
		tracker: tracker,
		w:       w,
	}
}

func (dr *downloadTracker) Stop() {
	dr.pw.Stop()
}
