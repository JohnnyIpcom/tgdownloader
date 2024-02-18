package cmd

import (
	"fmt"

	"github.com/gotd/td/telegram/peers"
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
		Short: "List peers from a chat/channel",
		Long:  "List peers from a chat/channel",
		Annotations: map[string]string{
			"prompt_suggest": "chatorchannel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			tdLibPeerID, err := r.parseTDLibPeerID(args[0])
			if err != nil {
				r.log.Error(err, "failed to convert peer ID")
				return err
			}

			switch {
			case tdLibPeerID.IsChat():
				users, total, err := r.client.UserService.GetUsersFromChat(cmd.Context(), tdLibPeerID.ToPlain())
				if err != nil {
					r.log.Error(err, "failed to get users")
					return err
				}

				return renderer.RenderUserTableAsync(cmd.Context(), users, total)

			case tdLibPeerID.IsChannel():
				users, total, err := r.client.UserService.GetUsersFromChannel(cmd.Context(), tdLibPeerID.ToPlain(), telegram.QueryRecent())
				if err != nil {
					r.log.Error(err, "failed to get users")
					return err
				}

				return renderer.RenderUserTableAsync(cmd.Context(), users, total)

			case tdLibPeerID.IsUser():
				user, err := r.client.UserService.GetUser(cmd.Context(), tdLibPeerID.ToPlain())
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

			tdLibPeerID := peer.TDLibPeerID()
			switch {
			case tdLibPeerID.IsUser():
				user, err := r.client.UserService.GetUser(cmd.Context(), peer.ID())
				if err != nil {
					return err
				}

				renderer.RenderUser(user)
				return nil

			case tdLibPeerID.IsChat() || tdLibPeerID.IsChannel():
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
			tdLibPeerID, err := r.parseTDLibPeerID(args[0])
			if err != nil {
				r.log.Error(err, "failed to convert peer ID")
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error(err, "failed to get user flag")
				return err
			}

			switch {
			case tdLibPeerID.IsChannel():
				users, total, err := r.client.UserService.GetUsersFromChannel(cmd.Context(), tdLibPeerID.ToPlain(), telegram.QuerySearch(userQuery))
				if err != nil {
					r.log.Error(err, "failed to get users")
					return err
				}

				return renderer.RenderUserTableAsync(cmd.Context(), users, total)

			case tdLibPeerID.IsChat() || tdLibPeerID.IsUser():
				return fmt.Errorf("unsupported peer type")
			}

			return nil
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
			tdLibPeerID, err := r.parseTDLibPeerID(args[0])
			if err != nil {
				r.log.Error(err, "failed to convert peer ID")
				return err
			}

			peer, err := r.client.PeerService.ResolveTDLibID(cmd.Context(), tdLibPeerID)
			if err != nil {
				r.log.Error(err, "failed to resolve peer")
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

	peer.AddCommand(peerListCmd)
	peer.AddCommand(peerResolveCmd)
	peer.AddCommand(peerFindCmd)
	peer.AddCommand(peerFromHistoryCmd)
	return peer
}
