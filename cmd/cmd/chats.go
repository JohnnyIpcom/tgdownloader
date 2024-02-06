package cmd

import (
	"strconv"

	"github.com/gotd/td/constant"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
)

func (r *Root) newChatCmd() *cobra.Command {
	chat := &cobra.Command{
		Use:   "chat",
		Short: "Manage chats",
		Long:  `Manage chats that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users in a chat",
		Long:  `Manage users in a chat that the user is a member of.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	userListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users in a chat",
		Long:  `List all users in a chat that the user is a member of.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "chat",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert chat ID")
				return err
			}

			users, total, err := r.client.UserService.GetUsersFromChat(cmd.Context(), chatID)
			if err != nil {
				r.log.Error(err, "failed to get users")
				return err
			}

			return renderer.RenderUserTableAsync(cmd.Context(), users, total)
		},
	}

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat",
		Long:  `Download files from a chat.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
	}

	var opts downloadOptions
	downloadHistoryCmd := &cobra.Command{
		Use:   "history",
		Short: "Download files from a chat history",
		Long:  `Download files from a chat history.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "chat",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert chat ID")
				return err
			}

			var tdLibPeerID constant.TDLibPeerID
			tdLibPeerID.Chat(ID)

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
		Short: "Watch a chat for new files",
		Long:  `Watch a chat for new files.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "chat",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error(err, "failed to convert chat ID")
				return err
			}

			return r.downloadFilesFromNewMessages(cmd.Context(), ID, opts)
		},
	}

	downloadWatcherCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(userListCmd)

	chat.AddCommand(userCmd)
	chat.AddCommand(downloadCmd)
	return chat
}
