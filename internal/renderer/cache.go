package renderer

import (
	"context"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"golang.org/x/sync/errgroup"
)

func RenderCachedPeerTable(cacheInfos []telegram.PeerCacheInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"Access Hash",
			"Created At",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, cacheInfo := range cacheInfos {
		t.AppendRow(table.Row{
			cacheInfo.ID,
			cacheInfo.AccessHash,
			cacheInfo.CreatedAt.Format(time.RFC3339),
		})
	}

	t.Render()
}

func RenderCachedPeerTableAsync(ctx context.Context, d <-chan telegram.PeerCacheInfo) error {
	pw := progress.NewWriter()
	pw.SetAutoStop(true)
	pw.SetTrackerLength(25)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetSortBy(progress.SortByPercentDsc)
	pw.SetStyle(progress.StyleDefault)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Visibility.ETA = true
	pw.Style().Visibility.ETAOverall = true

	go pw.Render()

	tracker := &progress.Tracker{
		Message: "Fetching cached peers...",
		Units:   progress.UnitsDefault,
	}

	pw.AppendTracker(tracker)
	var cachedInfos []telegram.PeerCacheInfo

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		RenderCachedPeerTable(cachedInfos)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case cacheInfo, ok := <-d:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				cachedInfos = append(cachedInfos, cacheInfo)
			}
		}
	})

	if err := g.Wait(); err != nil {
		tracker.MarkAsErrored()
		return err
	}

	tracker.MarkAsDone()
	return nil
}
