package cmd

import (
	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"

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

	peerFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a peer",
		Long:  `Find a peer by its data.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.client.PeerService.Resolve(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			tdLibPeerID := peer.TDLibPeerID()
			if tdLibPeerID.IsUser() {
				user, err := r.client.UserService.GetUser(cmd.Context(), peer.ID())
				if err != nil {
					return err
				}

				renderer.RenderUser(user)
				return nil
			}

			if tdLibPeerID.IsChat() || tdLibPeerID.IsChannel() {
				renderer.RenderPeerTable([]peers.Peer{peer})
				return nil
			}

			return nil
		},
	}

	peer.AddCommand(peerFindCmd)
	return peer
}
