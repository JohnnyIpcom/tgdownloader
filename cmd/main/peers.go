package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func getTypeTextByType(t telegram.ChatType) string {
	switch t {
	case telegram.ChatTypeChat:
		return "Chat"
	case telegram.ChatTypeChannel:
		return "Channel"
	default:
		return "Unknown"
	}
}

func renderChatTable(chats []telegram.ChatInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Title", "ID", "Type"})
	t.SortBy([]table.SortBy{
		{Name: "Title", Mode: table.Asc},
	})

	for _, chat := range chats {
		t.AppendRow(table.Row{chat.Title, chat.ID, getTypeTextByType(chat.Type)})
	}

	t.Render()
}

func renderUserTable(users []telegram.UserInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(table.Row{"ID", "Username", "First Name", "Last Name"})
	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, user := range users {
		t.AppendRow(table.Row{user.ID, user.Username, user.FirstName, user.LastName})
	}

	t.Render()
}

func renderUser(user telegram.UserInfo) {
	fmt.Printf("ID: %d, Username: %s, First Name: %s, Last Name: %s\n", user.ID, user.Username, user.FirstName, user.LastName) // TODO: use text pretty
}

func newPeerCmd(ctx context.Context, r *Root) *cobra.Command {
	peer := &cobra.Command{
		Use:   "peer",
		Short: "Manage peers(chats/channels)",
		Long:  `Manage peers(chats or channels) that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	peerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List chats/channels",
		Long:  `List chats and channels that the user is a member of.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				chats, err := c.GetAllChats(ctx)
				if err != nil {
					return err
				}

				renderChatTable(chats)
				return nil
			})
		},
	}

	peer.AddCommand(peerListCmd)
	return peer
}

func newChatCmd(ctx context.Context, r *Root) *cobra.Command {
	chat := &cobra.Command{
		Use:   "chat",
		Short: "Manage chats",
		Long:  `Manage chats that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	usersCmd := &cobra.Command{
		Use:   "users",
		Short: "Get users in a chat",
		Long:  `Get all users in a chat.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, _, err := c.GetAllUsersFromChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	finduserCmd := &cobra.Command{
		Use:   "finduser",
		Short: "Find a user in a chat",
		Long:  `Find a user in a chatl by its username/visible name.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, err := c.GetUsersFromChat(ctx, chatID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	finduserCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	finduserCmd.MarkFlagRequired("user")

	chat.AddCommand(usersCmd)
	chat.AddCommand(finduserCmd)
	return chat
}

func newChannelCmd(ctx context.Context, r *Root) *cobra.Command {
	channel := &cobra.Command{
		Use:   "channel",
		Short: "Manage channels",
		Long:  `Manage channels that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	usersCmd := &cobra.Command{
		Use:   "users",
		Short: "Get users in a channel",
		Long:  `Get all users in a channel.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert channelID", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, _, err := c.GetAllUsersFromChannel(ctx, channelID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	usersmCmd := &cobra.Command{
		Use:   "usersm",
		Short: "Get users in a chat",
		Long:  `Get all users in a chat.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				usersChan, errors := c.GetUsersFromMessageHistory(ctx, chatID, userQuery)
				g, ctx := errgroup.WithContext(ctx)
				g.Go(func() error {
					for {
						select {
						case <-ctx.Done():
							return ctx.Err()

						case err, ok := <-errors:
							if !ok {
								return nil
							}
							r.log.Error("failed to get user", zap.Error(err))

						case user, ok := <-usersChan:
							if !ok {
								return nil
							}

							renderUser(user)
						}
					}
				})

				return g.Wait()
			})
		},
	}

	usersmCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")

	finduserCmd := &cobra.Command{
		Use:   "finduser",
		Short: "Find a user in a channel",
		Long:  `Find a user in a channel by its username/first name/last name.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert channelID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, err := c.GetUsersFromChannel(ctx, channelID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	finduserCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	finduserCmd.MarkFlagRequired("user")

	channel.AddCommand(usersCmd)
	channel.AddCommand(usersmCmd)
	channel.AddCommand(finduserCmd)
	return channel
}
