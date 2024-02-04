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

func RenderCachedPeerTable(peers []telegram.CachedPeer) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"Type",
			"Name",
			"Access Hash",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, peer := range peers {
		peerType := "Unknown"
		if peer.User != nil {
			peerType = "User"
		} else if peer.Chat != nil {
			peerType = "Chat"
		} else if peer.Channel != nil {
			peerType = "Channel"
		}

		t.AppendRow(table.Row{
			peer.Key.ID,
			peerType,
			ReplaceAllEmojis(peer.Name()),
			peer.Key.AccessHash,
		})
	}

	t.Render()
}

func RenderCachedPeerTableAsync(ctx context.Context, d <-chan telegram.CachedPeer) error {
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
	var cachedPeers []telegram.CachedPeer

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		RenderCachedPeerTable(cachedPeers)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case peer, ok := <-d:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				cachedPeers = append(cachedPeers, peer)
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
