package renderer

import (
	"context"
	"io"
	"time"

	"github.com/fatih/color"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

const (
	// DefaultWidth is the default width of the progress bar.
	DefaultWidth = 80
	// DefaultNameWidth is the default width of the name column.
	DefaultNameWidth = 45
)

type Progress interface {
	EnablePS(ctx context.Context)
	Wait(ctx context.Context)
	WaitAndStop(ctx context.Context)

	UnitsTracker(message string, total int) Tracker
	BytesTracker(writer io.Writer, message string, total int64) BytesTracker
}

type progressImpl struct {
	p *mpb.Progress
}

var _ Progress = (*progressImpl)(nil)

func NewProgress(ctx context.Context) Progress {
	p := mpb.NewWithContext(
		ctx,
		mpb.WithOutput(color.Output),
		mpb.WithWidth(DefaultWidth),
		mpb.WithAutoRefresh(),
	)
	return &progressImpl{p: p}
}

func (p *progressImpl) EnablePS(ctx context.Context) {
	go func() {
		f := func() { /*p.SetPinnedMessages(strings.Join(ps.Humanize(ctx), " "))*/ }
		f()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				//p.SetPinnedMessages()
				return

			case <-ticker.C:
				f()
			}
		}
	}()
}

func (p *progressImpl) Wait(ctx context.Context) {
}

func (p *progressImpl) WaitAndStop(ctx context.Context) {
	p.p.Wait()
}

type Tracker interface {
	Increment(n int64)
	Fail()
	Done()
}

type tracker struct {
	bar   *mpb.Bar
	total int64
}

func (t *tracker) Increment(n int64) {
	t.bar.IncrInt64(n)
}

func (t *tracker) Fail() {
	t.bar.Abort(false)
}

func (t *tracker) Done() {
	t.bar.SetCurrent(t.total)
}

var _ Tracker = (*tracker)(nil)

var (
	clRed   = color.New(color.FgRed)
	clGreen = color.New(color.FgGreen)
)

// $TMP: copied from github.com/vbauerster/mpb/v8/decor/on_abort.go
// workaround for s.Completed -> s.Aborted
func OnAbortMeta(decorator decor.Decorator, fn func(string) string) decor.Decorator {
	if decorator == nil {
		return nil
	}
	return onAbortMetaWrapper{decorator, fn}
}

type onAbortMetaWrapper struct {
	decor.Decorator
	fn func(string) string
}

func (d onAbortMetaWrapper) Decor(s decor.Statistics) (string, int) {
	if s.Aborted {
		str, width := d.Decorator.Decor(s)
		return d.fn(str), width
	}
	return d.Decorator.Decor(s)
}

func (d onAbortMetaWrapper) Unwrap() decor.Decorator {
	return d.Decorator
}

func (p *progressImpl) UnitsTracker(message string, size int) Tracker {
	bar := p.p.AddBar(int64(size),
		mpb.PrependDecorators(
			decor.Name(text.Snip(message, DefaultNameWidth, "..."), decor.WC{W: DefaultNameWidth, C: decor.DindentRight}),
		),
		mpb.AppendDecorators(
			OnAbortMeta(
				decor.OnAbort(
					decor.OnCompleteMeta(
						decor.OnComplete(
							decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
							" done!",
						),
						toMetaFunc(clGreen),
					),
					" fail!",
				),
				toMetaFunc(clRed),
			),
		),
	)

	return &tracker{
		bar:   bar,
		total: int64(size),
	}
}

type BytesTracker interface {
	io.Writer
	Tracker
}

type bytesTracker struct {
	*tracker
	writer io.Writer
}

var (
	_ io.Writer = (*bytesTracker)(nil)
	_ Tracker   = (*bytesTracker)(nil)
)

func (bt *bytesTracker) Write(p []byte) (int, error) {
	return bt.writer.Write(p)
}

func (p *progressImpl) BytesTracker(writer io.Writer, message string, total int64) BytesTracker {
	bar := p.p.AddBar(total,
		mpb.PrependDecorators(
			decor.Name(text.Snip(message, DefaultNameWidth, "..."), decor.WC{W: DefaultNameWidth, C: decor.DindentRight}),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte(" %6.1f / %6.1f", decor.WCSyncWidth),
			OnAbortMeta(
				decor.OnAbort(
					decor.OnCompleteMeta(
						decor.OnComplete(
							decor.NewPercentage("%.1f", decor.WCSyncSpace),
							" done!",
						),
						toMetaFunc(clGreen),
					),
					" fail!",
				),
				toMetaFunc(clRed),
			),
		),
	)

	return &bytesTracker{
		tracker: &tracker{
			bar:   bar,
			total: total,
		},

		writer: bar.ProxyWriter(writer),
	}
}

func toMetaFunc(c *color.Color) func(string) string {
	return func(s string) string {
		return c.Sprint(s)
	}
}
