package renderer

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"

	"github.com/johnnyipcom/tgdownloader/pkg/ps"
)

type Progress interface {
	progress.Writer
	EnablePS(ctx context.Context)
	WaitAndStop(ctx context.Context)

	UnitsTracker(message string, total int) Tracker
	BytesTracker(writer io.Writer, message string, total int64) BytesTracker
}

type progressImpl struct {
	progress.Writer
}

var _ Progress = (*progressImpl)(nil)

func NewProgress() Progress {
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
	return &progressImpl{Writer: pw}
}

func (p *progressImpl) EnablePS(ctx context.Context) {
	go func() {
		f := func() { p.SetPinnedMessages(strings.Join(ps.Humanize(ctx), " ")) }
		f()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				p.SetPinnedMessages()
				return

			case <-ticker.C:
				f()
			}
		}
	}()
}

func (p *progressImpl) WaitAndStop(ctx context.Context) {
	for p.IsRenderInProgress() {
		if p.LengthActive() == 0 {
			p.Stop()
			return
		}

		time.Sleep(100 * time.Millisecond)
	}
}

type Tracker interface {
	Increment(n int64)
	Fail()
	Done()
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

var _ Tracker = (*tracker)(nil)

func (p *progressImpl) UnitsTracker(message string, size int) Tracker {
	tracker := &tracker{
		Tracker: &progress.Tracker{
			Message: message,
			Total:   int64(size),
			Units:   progress.UnitsDefault,
		},
	}

	p.Writer.AppendTracker(tracker.Tracker)
	return tracker
}

type BytesTracker interface {
	io.Writer
	Tracker
}

type bytesTracker struct {
	*tracker
	writer io.Writer
}

var _ io.Writer = (*bytesTracker)(nil)
var _ Tracker = (*bytesTracker)(nil)

func (bt *bytesTracker) Write(p []byte) (int, error) {
	n, err := bt.writer.Write(p)
	if err != nil {
		bt.tracker.MarkAsErrored()
		return n, err
	}

	bt.tracker.Increment(int64(n))
	return n, nil
}

func (p *progressImpl) BytesTracker(writer io.Writer, message string, total int64) BytesTracker {
	tracker := &bytesTracker{
		writer: writer,
		tracker: &tracker{
			Tracker: &progress.Tracker{
				Message: message,
				Total:   total,
				Units:   progress.UnitsBytes,
			},
		},
	}

	p.Writer.AppendTracker(tracker.Tracker)
	return tracker
}
