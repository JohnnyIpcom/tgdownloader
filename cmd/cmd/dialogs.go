package cmd

import (
	"fmt"

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
			peers, err := r.client.DialogCache.GetDialogPeers(cmd.Context())
			if err != nil {
				return err
			}

			renderer.RenderDialogsTable(peers)
			return nil
		},
	}

	dialogRefreshCmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh dialogs",
		Long:  "Refresh dialogs from Telegram",
		RunE: func(cmd *cobra.Command, args []string) error {
			dialogs, total, err := r.client.DialogService.GetAllDialogs(cmd.Context())
			if err != nil {
				return err
			}
			var tracker renderer.Tracker
			if r.progress != nil {
				tracker = r.progress.UnitsTracker("Refreshing dialogs", total)
			}
			for dialog := range dialogs {
				if dialog.Err() != nil {
					if tracker != nil {
						tracker.Fail()
					}
					return dialog.Err()
				}
				if tracker != nil {
					tracker.Increment(1)
				}
			}
			if err := cmd.Context().Err(); err != nil {
				if tracker != nil {
					tracker.Fail()
				}
				return err
			}
			if tracker != nil {
				tracker.Done()
				r.progress.Wait(cmd.Context())
			}

			peers, err := r.client.DialogCache.GetDialogPeers(cmd.Context())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Refreshed %d dialogs\n", len(peers))
			return err
		},
	}

	dialogCmd.AddCommand(dialogListCmd, dialogRefreshCmd)

	r.setupConnectionForCmd(dialogRefreshCmd)
	return dialogCmd
}
