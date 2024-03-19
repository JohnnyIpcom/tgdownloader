package renderer

import (
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type progressRenderer struct {
	pw progress.Writer
}

var _ telegram.ProgressRenderer = &progressRenderer{}

type ProgressTrackerOption func(*progressRenderer)

func NewProgressRenderer(opts ...ProgressTrackerOption) telegram.ProgressRenderer {
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

	rndr := &progressRenderer{pw: pw}
	for _, opt := range opts {
		opt(rndr)
	}

	return rndr
}

func (r *progressRenderer) Wait() {
	for r.pw.IsRenderInProgress() {
		if r.pw.LengthActive() == 0 {
			return
		}

		time.Sleep(time.Millisecond * 100)
	}
}

func (r *progressRenderer) Stop() {
	for r.pw.IsRenderInProgress() {
		if r.pw.LengthActive() == 0 {
			r.pw.Stop()
			return
		}

		time.Sleep(time.Millisecond * 100)
	}
}

type tracker struct {
	*progress.Tracker
}

func (t *tracker) Fail() {
	t.MarkAsErrored()
}

func (t *tracker) Done() {
	t.MarkAsDone()
}

func (r *progressRenderer) NewTracker(message string) telegram.Tracker {
	tracker := &tracker{
		Tracker: &progress.Tracker{
			Message: message,
			Units:   progress.UnitsDefault,
		},
	}

	r.pw.AppendTracker(tracker.Tracker)
	return tracker
}
