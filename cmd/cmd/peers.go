package cmd

import (
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/cobra"
)

func (r *Root) newPeerCmd() *cobra.Command {
	peer := &cobra.Command{
		Use:   "peer",
		Short: "Manage peers(chats/channels/users)",
		Long:  `Manage peers(chats/channels/users)`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	peerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all chats/channels",
		Long:  `List all chats and channels that the user is a member of.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			peers, err := r.client.PeerService.GetAllPeers(cmd.Context())
			if err != nil {
				return err
			}

			renderer.RenderPeerTable(peers)
			return nil
		},
	}

	peerFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a peer",
		Long:  `Find a peer by its data.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.client.PeerService.ResolvePeer(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			switch peer.Type {
			case telegram.PeerTypeUser:
				user, err := r.client.UserService.GetUser(cmd.Context(), peer.ID)
				if err != nil {
					return err
				}

				renderer.RenderUser(user)

			case telegram.PeerTypeChat:
				fallthrough
			case telegram.PeerTypeChannel:
				renderer.RenderPeerTable([]telegram.PeerInfo{peer})
			}
			return nil
		},
	}

	peer.AddCommand(peerListCmd)
	peer.AddCommand(peerFindCmd)
	return peer
}
