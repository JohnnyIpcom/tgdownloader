package cmd

import (
	"strconv"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
)

func (r *Root) newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
		Long:  `Manage users`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a user",
		Long:  `Download files from a user.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
	}

	var opts downloadOptions
	downloadHistoryCmd := &cobra.Command{
		Use:   "history",
		Short: "Download files from a user history",
		Long:  `Download files from a user history.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "user",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert user ID")
				return err
			}

			getFileOptions, err := opts.newGetFileOptions()
			if err != nil {
				r.log.Error(err, "failed to create get file options")
				return err
			}

			return r.downloadFiles(
				cmd.Context(),
				telegram.PeerInfo{
					ID:   ID,
					Type: telegram.PeerTypeUser,
				},
				opts.hashtags,
				getFileOptions...,
			)
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	downloadHistoryCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")

	downloadWatcherCmd := &cobra.Command{
		Use:   "watcher",
		Short: "Watch a user for new files",
		Long:  `Watch a user for new files.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "watcher",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert user ID")
				return err
			}

			return r.downloadFilesFromNewMessages(cmd.Context(), ID, opts.hashtags)
		},
	}

	downloadWatcherCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(downloadCmd)
	return userCmd
}
