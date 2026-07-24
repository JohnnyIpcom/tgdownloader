package renderer

import (
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

func RenderDialogsTable(peers []telegram.DialogPeer) string {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(table.Row{
		"Name",
		"ID",
		"TDLib Peer ID",
		"Type",
	})
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Name"),
	})
	t.SortBy([]table.SortBy{
		{Name: "Name", Mode: table.Asc},
	})

	for _, peer := range peers {
		t.AppendRow(table.Row{
			ReplaceAllEmojis(peer.Name()),
			peer.Key.ID,
			RenderTDLibPeerID(peer.TDLibPeerID()),
			dialogPeerTypename(peer),
		})
	}

	return t.Render()
}

func dialogPeerTypename(peer telegram.DialogPeer) string {
	switch {
	case peer.User != nil:
		return "User"
	case peer.Chat != nil:
		return "Chat"
	case peer.Channel != nil:
		return "Channel"
	default:
		return "Unknown"
	}
}
