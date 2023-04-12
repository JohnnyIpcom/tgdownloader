package cmd

import (
	"strconv"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
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

	type downloadOptions struct {
		limit      int
		user       int64
		offsetDate string
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
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			var getFileOptions []telegram.GetFileOption
			getFileOptions = append(getFileOptions, telegram.GetFileWithUserID(opts.user))

			if opts.limit > 0 {
				getFileOptions = append(getFileOptions, telegram.GetFileWithLimit(opts.limit))
			}

			if opts.offsetDate != "" {
				offsetDate, err := time.Parse("2006-01-02 15:04:05", opts.offsetDate)
				if err != nil {
					r.log.Error("failed to parse offset date", zap.Error(err))
					return err
				}

				getFileOptions = append(getFileOptions, telegram.GetFileWithOffsetDate(int(offsetDate.Unix())))
			}

			files, err := r.client.FileService.GetFiles(
				cmd.Context(),
				telegram.PeerInfo{
					ID:   ID,
					Type: telegram.PeerTypeUser,
				},
				getFileOptions...,
			)
			if err != nil {
				return err
			}

			downloader := r.getDownloader()
			downloader.Start(cmd.Context())
			downloader.AddDownloadQueue(cmd.Context(), files)
			return downloader.Stop()
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")

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
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			files, err := r.client.FileService.GetFilesFromNewMessages(cmd.Context(), ID)
			if err != nil {
				return err
			}

			downloader := r.getDownloader()
			downloader.Start(cmd.Context())
			downloader.AddDownloadQueue(cmd.Context(), files)
			return downloader.Stop()
		},
	}

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(downloadCmd)
	return userCmd
}
