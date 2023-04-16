package cmd

import (
	"strconv"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
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

			users, total, err := r.client.UserService.GetAllUsersFromChat(cmd.Context(), chatID)
			if err != nil {
				r.log.Error(err, "failed to get users")
				return err
			}

			return renderer.RenderUserTableAsync(cmd.Context(), users, total)
		},
	}

	userFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a user in a chat",
		Long:  `Find a user in a chat by its data.`,
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

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error(err, "failed to get user flag")
				return err
			}

			users, err := r.client.UserService.GetUsersFromChat(cmd.Context(), chatID, userQuery)
			if err != nil {
				r.log.Error(err, "failed to get users")
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

			getFileOptions, err := opts.newGetFileOptions()
			if err != nil {
				r.log.Error(err, "failed to get get file options")
				return err
			}

			return r.downloadFiles(
				cmd.Context(),
				telegram.PeerInfo{
					ID:   ID,
					Type: telegram.PeerTypeChat,
				},
				getFileOptions...,
			)
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")

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

			return r.downloadFilesFromNewMessages(cmd.Context(), ID)
		},
	}

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)

	chat.AddCommand(userCmd)
	chat.AddCommand(downloadCmd)
	return chat
}
