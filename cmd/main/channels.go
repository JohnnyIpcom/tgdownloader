package cmd

import (
	"context"
	"os"

	"github.com/gotd/td/tg"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func newChannelsCmd(ctx context.Context, r *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List chats/channels",
		Long:  `List chats and channels that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			r.client.Run(ctx, func(ctx context.Context, api *tg.Client) error {
				groupsClass, err := api.MessagesGetAllChats(ctx, []int64{})
				if err != nil {
					r.log.Error("get all chats", zap.Error(err))
					return err
				}

				t := table.NewWriter()
				t.SetOutputMirror(os.Stdout)
				t.AppendHeader(table.Row{"Title", "ID", "Type"})

				for _, chat := range groupsClass.GetChats() {
					switch c := chat.(type) {
					case *tg.Chat:
						t.AppendRow(table.Row{
							c.Title,
							c.ID,
							"Chat",
						})

					case *tg.Channel:
						t.AppendRow(table.Row{
							c.Title,
							c.ID,
							"Channel",
						})

					case *tg.ChatForbidden:
						t.AppendRow(table.Row{
							c.Title,
							c.ID,
							"ChatForbidden",
						})

					case *tg.ChannelForbidden:
						t.AppendRow(table.Row{
							c.Title,
							c.ID,
							"ChannelForbidden",
						})

					default:
						t.AppendRow(table.Row{
							"Unknown",
							"Unknown",
							"Unknown",
						})
					}
				}

				t.Render()
				return nil
			})
		},
	}

	return cmd
}
