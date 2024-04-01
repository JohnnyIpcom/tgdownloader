package renderer

import (
	"context"
	"os"
	"time"

	"github.com/gotd/td/telegram/peers"
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
			"TDLIB Peer ID",
			"Username",
			"Visible Name",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Visible Name"),
	})

	t.AppendRow(
		table.Row{
			user.ID(),
			RenderTDLibPeerID(user.TDLibPeerID()),
			getUsername(user),
			getVisibleName(user),
		},
	)

	return t.Render()
}

// RenderUserAsync renders a user one by one asynchronously.
func RenderUsersAsync(ctx context.Context, u <-chan peers.User) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(
		table.Row{
			"ID",
			"TDLIB Peer ID",
			"Username",
			"Visible Name",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		{
			Name:     "ID",
			WidthMin: 10,
			WidthMax: 10,
		},
		{
			Name:     "Username",
			WidthMin: 20,
			WidthMax: 20,
		},
		getVisibleNameConfig("Visible Name"),
	})

	ticker := time.NewTicker(time.Second * 1)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				if t.Length() > 0 {
					t.Render()
				}
				return ctx.Err()

			case <-ticker.C:
				if t.Length() > 0 {
					t.Render()
					t.ResetRows() // Reset rows to avoid duplicates
				}

			case user, ok := <-u:
				if !ok {
					if t.Length() > 0 {
						t.Render()
					}
					return nil
				}

				t.AppendRow(
					table.Row{
						user.ID(),
						RenderTDLibPeerID(user.TDLibPeerID()),
						getUsername(user),
						getVisibleName(user),
					},
				)
			}
		}
	})

	return g.Wait()
}

// RenderUserTable renders a table of users.
func RenderUserTable(users []peers.User) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"TDLIB Peer ID",
			"Username",
			"Visible Name",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Visible Name"),
	})

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, user := range users {
		t.AppendRow(
			table.Row{
				user.ID(),
				RenderTDLibPeerID(user.TDLibPeerID()),
				getUsername(user),
				getVisibleName(user),
			},
		)
	}

	t.Render()
}

// RenderUserTableAsync renders a table of users asynchronously.
func RenderUserTableAsync(ctx context.Context, u <-chan peers.User, total int) error {
	return renderAsync(ctx, u, "Fetching users...", total, RenderUserTable)
}
