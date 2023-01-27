package cmd

import (
	"context"

	"github.com/gotd/td/tg"
	"github.com/spf13/cobra"
)

func newDownloadCmd(ctx context.Context, r *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat, channel or user",
		Long:  `Download files from a chat, channel or user.`,
		Run: func(cmd *cobra.Command, args []string) {
			r.client.Run(ctx, func(ctx context.Context, c *tg.Client) error {
				return nil
			})
		},
	}

	/*cmd.AddCommand(newDownloadChatCmd())
	cmd.AddCommand(newDownloadChannelCmd())
	cmd.AddCommand(newDownloadUserCmd())*/

	return cmd
}
