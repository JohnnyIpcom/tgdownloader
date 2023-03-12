package renderer

import (
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
)

type DownloadRenderer struct {
	pw progress.Writer
}

type DownloadRendererOption func(*DownloadRenderer)

func WithNumTrackersExpected(n int) DownloadRendererOption {
	return func(dr *DownloadRenderer) {
		dr.pw.SetNumTrackersExpected(n)
	}
}

// NewDownloadRenderer creates a new download renderer.
func NewDownloadRenderer(opts ...DownloadRendererOption) *DownloadRenderer {
	pw := progress.NewWriter()
	pw.SetAutoStop(false)
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

	renderer := &DownloadRenderer{
		pw: pw,
	}

	for _, opt := range opts {
		opt(renderer)
	}

	return renderer
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

func (dr *DownloadRenderer) TrackedWriter(msg string, size int64, w io.Writer) TrackedWriter {
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

func (dr *DownloadRenderer) Stop() {
	dr.pw.Stop()
}
