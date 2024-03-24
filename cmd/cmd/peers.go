package cmd

import (
	"fmt"

	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/cobra"
)

func (r *Root) newPeerCmd() *cobra.Command {
	peerCmd := &cobra.Command{
		Use:   "peer",
		Short: "Manage peers(chats/channels/users)",
		Long:  `Manage peers(chats/channels/users)`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	peerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List peers from a chat/channel",
		Long:  "List peers from a chat/channel",
		Annotations: map[string]string{
			"prompt_suggest": "chatorchannel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.resolvePeer(cmd.Context(), args[0])
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			switch {
			case peer.TDLibPeerID().IsChat() || peer.TDLibPeerID().IsChannel():
				users, total, err := r.client.UserService.GetUsers(cmd.Context(), peer, telegram.QueryRecent())
				if err != nil {
					r.log.Error(err, "failed to get users")
					return err
				}

				return renderer.RenderUserTableAsync(cmd.Context(), users, total)

			case peer.TDLibPeerID().IsUser():
				user, err := r.client.UserService.GetUser(cmd.Context(), peer.ID())
				if err != nil {
					r.log.Error(err, "failed to get user")
					return err
				}

				renderer.RenderUser(user)
				return nil
			}

			return fmt.Errorf("unsupported peer type")
		},
	}

	peerResolveCmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a peer",
		Long:  `Resolve a peer by its data.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.client.PeerService.Resolve(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			switch {
			case peer.TDLibPeerID().IsUser():
				user, err := r.client.UserService.GetUser(cmd.Context(), peer.ID())
				if err != nil {
					return err
				}

				renderer.RenderUser(user)
				return nil

			default:
				renderer.RenderPeerTable([]peers.Peer{peer})
			}

			return nil
		},
	}

	peerFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a user in a channel",
		Long:  `Find a user in a channel by its data.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error(err, "failed to get user flag")
				return err
			}

			peer, err := r.resolvePeer(cmd.Context(), args[0])
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			switch {
			case peer.TDLibPeerID().IsChannel():
				users, total, err := r.client.UserService.GetUsers(cmd.Context(), peer, telegram.QuerySearch(userQuery))
				if err != nil {
					r.log.Error(err, "failed to get users")
					return err
				}

				return renderer.RenderUserTableAsync(cmd.Context(), users, total)

			default:
				return fmt.Errorf("unsupported peer type")
			}
		},
	}

	peerFindCmd.Flags().StringP("user", "u", "", "User query")

	peerFromHistoryCmd := &cobra.Command{
		Use:   "from-history",
		Short: "Get users from message history",
		Long:  `Get users from message history.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.resolvePeer(cmd.Context(), args[0])
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			usersChan, err := r.client.UserService.GetUsersFromMessageHistory(cmd.Context(), peer)
			if err != nil {
				r.log.Error(err, "failed to get users")
				return err
			}

			return renderer.RenderUsersAsync(cmd.Context(), usersChan)
		},
	}

	peerCmd.AddCommand(
		peerListCmd,
		peerResolveCmd,
		peerFindCmd,
		peerFromHistoryCmd,
	)

	r.setupConnectionForCmd(
		peerListCmd,
		peerResolveCmd,
		peerFindCmd,
		peerFromHistoryCmd,
	)
	return peerCmd
}
