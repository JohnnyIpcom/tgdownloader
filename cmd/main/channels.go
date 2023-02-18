package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	case telegram.ChatTypeChatForbidden:
		return "ChatForbidden"
	case telegram.ChatTypeChannelForbidden:
		return "ChannelForbidden"
	default:
		return "Unknown"
	}
}

func renderChatTable(chats []telegram.Chat) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Title", "ID", "Type"})
	t.SortBy([]table.SortBy{
		{Name: "Type", Mode: table.Asc},
		{Name: "Title", Mode: table.Asc},
	})

	for _, chat := range chats {
		t.AppendRow(table.Row{chat.Title, chat.ID, getTypeTextByType(chat.Type)})
	}

	t.Render()
}

func renderUserTable(users []telegram.User) {
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

func newChannelsCmd(ctx context.Context, r *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage chats/channels",
		Long:  `Manage chats and channels that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	listCmd := &cobra.Command{
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

	findCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a chat/channel",
		Long:  `Find a chat or channel by its title.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				chats, err := c.GetAllChats(ctx)
				if err != nil {
					return err
				}

				var foundChats []telegram.Chat
				for _, chat := range chats {
					if strings.Contains(chat.Title, args[0]) {
						foundChats = append(foundChats, chat)
					}
				}

				renderChatTable(foundChats)
				return nil
			})
		},
	}

	usersCmd := &cobra.Command{
		Use:   "users",
		Short: "Get users in a chat/channel",
		Long:  `Get all users in a chat or channel.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				chatID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					r.log.Error("failed to convert chatID", zap.Error(err))
					return err
				}

				chat, err := c.FindChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to find chat", zap.Error(err))
					return err
				}

				users, err := c.GetAllUsers(ctx, chat)
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
		Short: "Get users in a chat/channel (from messages)",
		Long:  `Parse a message history and get all users in a chat/channel.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				chatID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					r.log.Error("failed to convert chatID", zap.Error(err))
					return err
				}

				chat, err := c.FindChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to find chat", zap.Error(err))
					return err
				}

				users, errors := c.GetUsersFromMessageHistory(ctx, chat)

				g, ctx := errgroup.WithContext(ctx)
				g.Go(func() error {
					select {
					case <-ctx.Done():
						return ctx.Err()

					case err := <-errors:
						r.log.Error("failed to get users from message history", zap.Error(err))

					case user, ok := <-users:
						if !ok {
							return nil
						}

						fmt.Printf("%d %s %s %s\n", user.ID, user.Username, user.FirstName, user.LastName)
					}

					return nil
				})

				return g.Wait()
			})
		},
	}

	finduserCmd := &cobra.Command{
		Use:   "finduser",
		Short: "Find a user in a chat/channel",
		Long:  `Find a user in a chat or channel by its username/first name/last name.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
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

				chat, err := c.FindChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to find chat", zap.Error(err))
					return err
				}

				users, err := c.GetUsers(ctx, chat, userQuery, 0)
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

	cmd.AddCommand(listCmd)
	cmd.AddCommand(findCmd)
	cmd.AddCommand(usersCmd)
	cmd.AddCommand(usersmCmd)
	cmd.AddCommand(finduserCmd)
	return cmd
}
