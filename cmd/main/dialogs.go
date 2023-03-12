package cmd

import (
	"context"
	"strings"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/cobra"
)

func (r *Root) newDialogsCmd() *cobra.Command {
	dialogCmd := &cobra.Command{
		Use:   "dialog",
		Short: "Manage dialogs",
		Long:  "Manage dialogs",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	dialogListCmd := &cobra.Command{
		Use:   "list",
		Short: "List dialogs",
		Long:  "List dialogs",
		RunE: func(cmd *cobra.Command, args []string) error {
			filter, err := cmd.Flags().GetString("type")
			if err != nil {
				return err
			}

			return r.client.Run(cmd.Context(), func(ctx context.Context, client *telegram.Client) error {
				dialogs, total, err := client.DialogService.GetAllDialogs(ctx)
				if err != nil {
					return err
				}

				filterFuncs := []renderer.FilterDialogFunc{}
				if filter != "" {
					filterFuncs = append(filterFuncs, func(dialog telegram.DialogInfo) bool {
						return strings.EqualFold(dialog.Peer.Type.String(), filter)
					})
				}

				return renderer.RenderDialogsTableAsync(ctx, dialogs, total, filterFuncs...)
			})
		},
	}

	dialogListCmd.Flags().StringP("type", "t", "", "Filter by type(channel, chat, user)")

	dialogCmd.AddCommand(dialogListCmd)
	return dialogCmd
}
