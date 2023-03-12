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

type FilterDialogFunc func(telegram.DialogInfo) bool

func RenderDialogsTable(dialogs []telegram.DialogInfo, filterFuncs ...FilterDialogFunc) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"Name",
			"ID",
			"Type",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "Name", Mode: table.Asc},
	})

	for _, dialog := range dialogs {
		if dialog.Err() != nil {
			continue
		}

		skip := false
		if len(filterFuncs) > 0 {
			for _, filterFunc := range filterFuncs {
				if !filterFunc(dialog) {
					skip = true
					break
				}
			}
		}

		if !skip {
			t.AppendRow(
				table.Row{
					dialog.Peer.Name,
					dialog.Peer.ID,
					dialog.Peer.Type.String(),
				},
			)
		}
	}

	return t.Render()
}

func RenderDialogsTableAsync(ctx context.Context, d <-chan telegram.DialogInfo, total int, filterFunc ...FilterDialogFunc) error {
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
		Total:   int64(total),
		Message: "Fetching dialogs",
		Units:   progress.UnitsDefault,
	}

	pw.AppendTracker(tracker)
	var dialogs []telegram.DialogInfo

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		RenderDialogsTable(dialogs, filterFunc...)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case dialog, ok := <-d:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				dialogs = append(dialogs, dialog)
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
