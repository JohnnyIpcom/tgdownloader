package cmd

import (
	"errors"
	"os"

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

			return r.runPromptTUI(cmd.Context())
		},
	}
}
