package cmd

import (
	"strconv"
	"time"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func (r *Root) newChannelCmd() *cobra.Command {
	channel := &cobra.Command{
		Use:   "channel",
		Short: "Manage channels",
		Long:  `Manage channels that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users in a channel",
		Long:  `Manage users in a channel that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	userListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users in a channel",
		Long:  `List all users in a channel that the user is a member of.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert channelID", zap.Error(err))
				return err
			}

			u, total, err := r.client.UserService.GetAllUsersFromChannel(cmd.Context(), channelID)
			if err != nil {
				r.log.Error("failed to get users", zap.Error(err))
				return err
			}

			return renderer.RenderUserTableAsync(cmd.Context(), u, total)
		},
	}

	userFromHistoryCmd := &cobra.Command{
		Use:   "from-history",
		Short: "List all users in a channel from its message history",
		Long:  `List all users in a channel from its message history.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			usersChan, err := r.client.UserService.GetUsersFromMessageHistory(
				cmd.Context(),
				telegram.PeerInfo{
					ID:   channelID,
					Type: telegram.PeerTypeChannel,
				},
				userQuery,
			)

			if err != nil {
				r.log.Error("failed to get users", zap.Error(err))
				return err
			}

			renderer.RenderUserAsync(cmd.Context(), usersChan)
			return nil
		},
	}

	userFromHistoryCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")

	userFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a user in a channel",
		Long:  `Find a user in a channel by its data.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert channelID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			users, err := r.client.UserService.GetUsersFromChannel(cmd.Context(), channelID, userQuery)
			if err != nil {
				r.log.Error("failed to get users", zap.Error(err))
				return err
			}

			renderer.RenderUserTable(users)
			return nil
		},
	}

	userFindCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	userFindCmd.MarkFlagRequired("user")

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a channel",
		Long:  `Download files from a channel.`,
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
		Short: "Download files from a channel history",
		Long:  `Download files from a channel history.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
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
					Type: telegram.PeerTypeChannel,
				},
				getFileOptions...,
			)
			if err != nil {
				return err
			}

			downloader := r.getDownloader(cmd.Context())
			downloader.Start(cmd.Context())
			downloader.RunAsyncDownloader(cmd.Context(), files)
			return downloader.Stop(cmd.Context())
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")

	downloadWatcherCmd := &cobra.Command{
		Use:   "watcher",
		Short: "Watch a channel for new files",
		Long:  `Watch a channel for new files.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "channel",
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

			downloader := r.getDownloader(cmd.Context())
			downloader.Start(cmd.Context())
			downloader.RunAsyncDownloader(cmd.Context(), files)
			return downloader.Stop(cmd.Context())
		},
	}

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)
	userCmd.AddCommand(userFromHistoryCmd)

	channel.AddCommand(userCmd)
	channel.AddCommand(downloadCmd)
	return channel
}
