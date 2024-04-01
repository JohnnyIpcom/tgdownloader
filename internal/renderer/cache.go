package renderer

import (
	"context"
	"os"

	"github.com/gotd/td/constant"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

func RenderCachedPeerTable(peers []telegram.CachedPeer) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"TDLib Peer ID",
			"Type",
			"Name",
			"Access Hash",
		},
	)
	t.SetColumnConfigs([]table.ColumnConfig{
		getVisibleNameConfig("Name"),
	})

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, peer := range peers {
		var tdLibPeerID constant.TDLibPeerID
		peerType := "Unknown"
		switch {
		case peer.User != nil:
			peerType = "User"
			tdLibPeerID.User(peer.User.ID)

		case peer.Chat != nil:
			peerType = "Chat"
			tdLibPeerID.Chat(peer.Chat.ID)

		case peer.Channel != nil:
			peerType = "Channel"
			tdLibPeerID.Channel(peer.Channel.ID)
		}

		t.AppendRow(table.Row{
			peer.Key.ID,
			RenderTDLibPeerID(tdLibPeerID),
			peerType,
			RenderName(peer.Name()),
			RenderAccessHash(peer.Key.AccessHash),
		})
	}

	t.Render()
}

func RenderCachedPeerTableAsync(ctx context.Context, d <-chan telegram.CachedPeer, total int) error {
	return renderAsync(ctx, d, "Fetching cached peers...", total, RenderCachedPeerTable)
}
