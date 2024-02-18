package renderer

import (
	"os"

	"github.com/gotd/td/telegram/peers"
	"github.com/jedib0t/go-pretty/v6/table"
)

// RenderPeerTable renders a list of peers.
func RenderPeerTable(peers []peers.Peer) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"Visible Name",
			"ID",
			"TDLib Peer ID",
			"Type",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Visible Name"),
	})

	t.SortBy([]table.SortBy{
		{Name: "Name", Mode: table.Asc},
	})

	for _, peer := range peers {
		t.AppendRow(
			table.Row{
				getVisibleName(peer),
				peer.ID(),
				RenderTDLibPeerID(peer.TDLibPeerID()),
				getPeerTypename(peer),
			},
		)
	}

	return t.Render()
}
