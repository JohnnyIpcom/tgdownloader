package renderer

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/gotd/td/telegram/peers"
	"golang.org/x/sync/errgroup"
)

// RenderUser renders a single user.
func RenderUser(writer io.Writer, user peers.User) string {
	return renderTableData(writer, userTableData([]peers.User{user}, false))
}

// RenderUsersAsync renders users in periodic structured batches.
func RenderUsersAsync(ctx context.Context, writer io.Writer, u <-chan peers.User) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var pending []peers.User
	flush := func() {
		if len(pending) == 0 {
			return
		}
		renderTableData(writer, userTableData(pending, false))
		pending = nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				flush()
				return ctx.Err()
			case <-ticker.C:
				flush()
			case user, ok := <-u:
				if !ok {
					flush()
					return nil
				}
				pending = append(pending, user)
			}
		}
	})
	return g.Wait()
}

// RenderUserTable renders a table of users.
func RenderUserTable(writer io.Writer, users []peers.User) {
	renderTableData(writer, userTableData(users, true))
}

// RenderUserTableAsync renders a table of users asynchronously.
func RenderUserTableAsync(ctx context.Context, writer io.Writer, u <-chan peers.User, total int) error {
	return renderAsync(ctx, writer, u, "Fetching users...", total, RenderUserTable)
}

func userTableData(users []peers.User, indexed bool) TableData {
	users = append([]peers.User(nil), users...)
	sort.SliceStable(users, func(i, j int) bool { return users[i].ID() < users[j].ID() })
	data := TableData{Columns: []TableColumn{
		{Header: "ID", MinWidth: 3, Priority: 20, Align: TableAlignRight},
		{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
		{Header: "Username", MinWidth: 10, Priority: 30},
		{Header: "Visible Name", MinWidth: 12, Priority: 100, Required: true},
	}}
	if indexed {
		data.Columns = append([]TableColumn{{Header: "#", MinWidth: 2, Priority: 1, Align: TableAlignRight}}, data.Columns...)
	}
	for i, user := range users {
		row := []string{
			fmt.Sprintf("%d", user.ID()),
			RenderTDLibPeerID(user.TDLibPeerID()),
			getUsername(user),
			getVisibleName(user),
		}
		if indexed {
			row = append([]string{fmt.Sprintf("%d", i+1)}, row...)
		}
		data.Rows = append(data.Rows, row)
	}
	return data
}
