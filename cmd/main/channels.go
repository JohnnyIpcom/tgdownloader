package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
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

	for _, chat := range chats {
		t.AppendRow(table.Row{chat.Title, chat.ID, getTypeTextByType(chat.Type)})
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

	cmd.AddCommand(listCmd)
	cmd.AddCommand(findCmd)
	return cmd
}
