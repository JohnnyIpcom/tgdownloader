package cmd

import (
	"errors"
	"os"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
)

func (r *Root) newExitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exit",
		Short: "Exit the prompt",
		Long:  "Exit the prompt",
		Run: func(*cobra.Command, []string) {
			_ = r.Close()
			os.Exit(0)
		},
	}
}

func (r *Root) newPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Start an interactive prompt",
		Long:  "Start an interactive prompt",
		RunE: func(cmd *cobra.Command, _ []string) (runErr error) {
			defer func() {
				runErr = errors.Join(runErr, r.Close())
			}()

			ctx := cmd.Context()
			if err := r.Connect(ctx); err != nil {
				return err
			}

			setupTracker := r.progress.UnitsTracker("Prompt setup", 0)
			self, err := r.client.UserService.GetSelf(ctx)
			if err != nil {
				setupTracker.Fail()
				r.progress.Wait(ctx)
				return err
			}

			enabled, path, maxEntries := r.promptHistorySettings()
			var history *promptHistoryStore
			if enabled {
				history, err = newPromptHistoryStore(path, maxEntries, r.shouldSkipPromptHistoryEntry)
				if err != nil {
					renderer.RenderError(cmd.OutOrStdout(), err)
					history = nil
				}
			}

			setupTracker.Done()
			r.progress.Wait(ctx)
			return r.runPromptTUI(ctx, history, self.Raw().Username)
		},
	}
}
