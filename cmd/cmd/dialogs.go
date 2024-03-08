package cmd

import (
	"github.com/johnnyipcom/tgdownloader/internal/renderer"

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
			dialogs, total, err := r.client.DialogService.GetAllDialogs(cmd.Context())
			if err != nil {
				return err
			}

			return renderer.RenderDialogsTableAsync(cmd.Context(), dialogs, total)
		},
	}

	dialogCmd.AddCommand(dialogListCmd)
	return dialogCmd
}
