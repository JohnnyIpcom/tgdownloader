package renderer

import (
	"context"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type FilterDialogFunc func(telegram.Dialog) bool

func RenderDialogsTable(dialogs []telegram.Dialog, filterFuncs ...FilterDialogFunc) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"Name",
			"ID",
			"TDLib Peer ID",
			"Type",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Name"),
	})

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
					getVisibleName(dialog.Peer),
					dialog.Peer.ID(),
					RenderTDLibPeerID(dialog.Peer.TDLibPeerID()),
					getPeerTypename(dialog.Peer),
				},
			)
		}
	}

	return t.Render()
}

func RenderDialogsTableAsync(ctx context.Context, d <-chan telegram.Dialog, total int, filterFunc ...FilterDialogFunc) error {
	return renderAsync(ctx, d, "Fetching dialogs...", total, func(dialogs []telegram.Dialog) {
		RenderDialogsTable(dialogs, filterFunc...)
	})
}
