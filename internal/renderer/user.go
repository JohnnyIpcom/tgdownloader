package renderer

import (
	"context"
	"os"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/sync/errgroup"
)

// RenderUser renders a single user.
func RenderUser(user peers.User) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(
		table.Row{
			"ID",
			"Username",
			"Visible Name",
		},
	)

	t.AppendRow(
		table.Row{
			user.ID(),
			getUsername(user),
			getVisibleName(user),
		},
	)

	return t.Render()
}

// RenderUserAsync renders a user one by one asynchronously.
func RenderUserAsync(ctx context.Context, u <-chan peers.User) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case user, ok := <-u:
				if !ok {
					return nil
				}

				RenderUser(user)
			}
		}
	})

	return g.Wait()
}

// RenderUserTable renders a table of users.
func RenderUserTable(users []peers.User) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"Username",
			"Visible Name",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, user := range users {
		t.AppendRow(
			table.Row{
				user.ID(),
				getUsername(user),
				getVisibleName(user),
			},
		)
	}

	return t.Render()
}

// RenderUserTableAsync renders a table of users asynchronously.
func RenderUserTableAsync(ctx context.Context, u <-chan peers.User, total int) error {
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
		Message: "Fetching users",
		Units:   progress.UnitsDefault,
	}

	pw.AppendTracker(tracker)
	var users []peers.User

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		RenderUserTable(users)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case user, ok := <-u:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				users = append(users, user)
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
