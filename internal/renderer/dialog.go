package renderer

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

func RenderDialogsTable(writer io.Writer, peers []telegram.DialogPeer) string {
	peers = append([]telegram.DialogPeer(nil), peers...)
	sort.SliceStable(peers, func(i, j int) bool {
		return strings.ToLower(peers[i].Name()) < strings.ToLower(peers[j].Name())
	})

	data := TableData{Columns: []TableColumn{
		{Header: "#", MinWidth: 2, Priority: 1, Align: TableAlignRight},
		{Header: "Name", MinWidth: 12, Priority: 100, Required: true},
		{Header: "ID", MinWidth: 3, Priority: 10, Align: TableAlignRight},
		{Header: "TDLib Peer ID", MinWidth: 18, Priority: 100, Required: true},
		{Header: "Type", MinWidth: 4, Priority: 20},
	}}
	for i, peer := range peers {
		data.Rows = append(data.Rows, []string{
			fmt.Sprintf("%d", i+1),
			peer.Name(),
			fmt.Sprintf("%d", peer.Key.ID),
			RenderTDLibPeerID(peer.TDLibPeerID()),
			dialogPeerTypename(peer),
		})
	}
	return renderTableData(writer, data)
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
