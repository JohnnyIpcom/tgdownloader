package cmd

import (
	"strconv"

	"github.com/gotd/td/constant"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
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
				r.log.Error(err, "failed to convert channel ID")
				return err
			}

			u, total, err := r.client.UserService.GetUsersFromChannel(cmd.Context(), channelID, telegram.QueryRecent())
			if err != nil {
				r.log.Error(err, "failed to get users")
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
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert channel ID")
				return err
			}

			var tdLibPeerID constant.TDLibPeerID
			tdLibPeerID.Channel(ID)

			peer, err := r.client.PeerService.ResolveTDLibID(cmd.Context(), tdLibPeerID)
			if err != nil {
				r.log.Error(err, "failed to resolve peer")
				return err
			}

			usersChan, err := r.client.UserService.GetUsersFromMessageHistory(cmd.Context(), peer)
			if err != nil {
				r.log.Error(err, "failed to get users")
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
				r.log.Error(err, "failed to convert channel ID")
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error(err, "failed to get user flag")
				return err
			}

			users, total, err := r.client.UserService.GetUsersFromChannel(cmd.Context(), channelID, telegram.QuerySearch(userQuery))
			if err != nil {
				r.log.Error(err, "failed to get users")
				return err
			}

			renderer.RenderUserTableAsync(cmd.Context(), users, total)
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
				r.log.Error(err, "failed to convert channel ID")
				return err
			}

			var tdLibPeerID constant.TDLibPeerID
			tdLibPeerID.Channel(ID)

			peer, err := r.client.PeerService.ResolveTDLibID(cmd.Context(), tdLibPeerID)
			if err != nil {
				r.log.Error(err, "failed to resolve peer")
				return err
			}

			return r.downloadFiles(cmd.Context(), peer, opts)
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	downloadHistoryCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadHistoryCmd.Flags().BoolVar(&opts.saveOnlyIfNew, "save-only-if-new", false, "Save only if new")

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
				r.log.Error(err, "failed to convert channel ID")
				return err
			}

			return r.downloadFilesFromNewMessages(cmd.Context(), ID, opts)
		},
	}

	downloadWatcherCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)
	userCmd.AddCommand(userFromHistoryCmd)

	channel.AddCommand(userCmd)
	channel.AddCommand(downloadCmd)
	return channel
}
