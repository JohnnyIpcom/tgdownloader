package renderer

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gotd/td/telegram/peers"
)

// RenderPeerTable renders a list of peers.
func RenderPeerTable(writer io.Writer, items []peers.Peer) {
	items = append([]peers.Peer(nil), items...)
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(getVisibleName(items[i])) < strings.ToLower(getVisibleName(items[j]))
	})

	data := TableData{Columns: []TableColumn{
		{Header: "#", MinWidth: 2, Priority: 1, Align: TableAlignRight},
		{Header: "Visible Name", MinWidth: 12, Priority: 100, Required: true},
		{Header: "ID", MinWidth: 3, Priority: 10, Align: TableAlignRight},
		{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
		{Header: "Type", MinWidth: 4, Priority: 20},
	}}
	for i, peer := range items {
		data.Rows = append(data.Rows, []string{
			fmt.Sprintf("%d", i+1),
			getVisibleName(peer),
			fmt.Sprintf("%d", peer.ID()),
			RenderTDLibPeerID(peer.TDLibPeerID()),
			getPeerTypename(peer),
		})
	}
	renderTableData(writer, data)
}

func RenderPeerTableAsync(ctx context.Context, writer io.Writer, u <-chan peers.Peer, total int) error {
	return renderAsync(ctx, writer, u, "Fetching peers...", total, RenderPeerTable)
}
