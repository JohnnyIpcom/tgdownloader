package renderer

import (
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

// RenderPeerTable renders a list of peers.
func RenderPeerTable(chats []telegram.PeerInfo) string {
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

	for _, chat := range chats {
		t.AppendRow(
			table.Row{
				ReplaceAllEmojis(chat.Name),
				chat.ID,
				chat.Type.String(),
			},
		)
	}

	return t.Render()
}
