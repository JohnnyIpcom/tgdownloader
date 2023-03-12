package cmd

import (
	"context"
	"strconv"
	"time"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func (r *Root) newPeerCmd() *cobra.Command {
	peer := &cobra.Command{
		Use:   "peer",
		Short: "Manage peers(chats/channels/users)",
		Long:  `Manage peers(chats/channels/users)`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	peerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all chats/channels",
		Long:  `List all chats and channels that the user is a member of.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				peers, err := c.PeerService.GetAllPeers(ctx)
				if err != nil {
					return err
				}

				renderer.RenderPeerTable(peers)
				return nil
			})
		},
	}

	peerFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a peer",
		Long:  `Find a peer by its data.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				peer, err := c.PeerService.ResolvePeer(ctx, args[0])
				if err != nil {
					return err
				}

				switch peer.Type {
				case telegram.PeerTypeUser:
					user, err := c.UserService.GetUser(ctx, peer.ID)
					if err != nil {
						return err
					}

					renderer.RenderUser(user)

				case telegram.PeerTypeChat:
					fallthrough
				case telegram.PeerTypeChannel:
					renderer.RenderPeerTable([]telegram.PeerInfo{peer})
				}
				return nil
			})
		},
	}

	peer.AddCommand(peerListCmd)
	peer.AddCommand(peerFindCmd)
	return peer
}

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
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				users, total, err := c.UserService.GetAllUsersFromChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				return renderer.RenderUserTableAsync(ctx, users, total)
			})
		},
	}

	userFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a user in a chat",
		Long:  `Find a user in a chat by its data.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chatID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			userQuery, err := cmd.Flags().GetString("user")
			if err != nil {
				r.log.Error("failed to get user flag", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				users, err := c.UserService.GetUsersFromChat(ctx, chatID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderer.RenderUserTable(users)
				return nil
			})
		},
	}

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat",
		Long:  `Download files from a chat.`,
		Args:  cobra.ExactArgs(1),
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
		Short: "Download files from a chat history",
		Long:  `Download files from a chat history.`,
		Args:  cobra.ExactArgs(1),
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

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFiles(
					ctx,
					telegram.PeerInfo{
						ID:   ID,
						Type: telegram.PeerTypeChat,
					},
					getFileOptions...,
				)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			})
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), r.client.WithUpdates(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFilesFromNewMessages(ctx, ID)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			}))
		},
	}

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userFindCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	userFindCmd.MarkFlagRequired("user")

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)

	chat.AddCommand(userCmd)
	chat.AddCommand(downloadCmd)
	return chat
}

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
		RunE: func(cmd *cobra.Command, args []string) error {
			channelID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert channelID", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				u, total, err := c.UserService.GetAllUsersFromChannel(ctx, channelID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				return renderer.RenderUserTableAsync(ctx, u, total)
			})
		},
	}

	userFromHistoryCmd := &cobra.Command{
		Use:   "from-history",
		Short: "List all users in a channel from its message history",
		Long:  `List all users in a channel from its message history.`,
		Args:  cobra.ExactArgs(1),
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

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				usersChan, err := c.UserService.GetUsersFromMessageHistory(
					ctx,
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

				renderer.RenderUserAsync(ctx, usersChan)
				return nil
			})
		},
	}

	userFromHistoryCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")

	userFindCmd := &cobra.Command{
		Use:   "find",
		Short: "Find a user in a channel",
		Long:  `Find a user in a channel by its data.`,
		Args:  cobra.ExactArgs(1),
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

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				users, err := c.UserService.GetUsersFromChannel(ctx, channelID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderer.RenderUserTable(users)
				return nil
			})
		},
	}

	userFindCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	userFindCmd.MarkFlagRequired("user")

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a channel",
		Long:  `Download files from a channel.`,
		Args:  cobra.ExactArgs(1),
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

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFiles(
					ctx,
					telegram.PeerInfo{
						ID:   ID,
						Type: telegram.PeerTypeChannel,
					},
					getFileOptions...,
				)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			})
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), r.client.WithUpdates(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFilesFromNewMessages(ctx, ID)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			}))
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
		Args:  cobra.ExactArgs(1),
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

			return r.client.Run(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFiles(
					ctx,
					telegram.PeerInfo{
						ID:   ID,
						Type: telegram.PeerTypeUser,
					},
					getFileOptions...,
				)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			})
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

			return r.client.Run(cmd.Context(), r.client.WithUpdates(cmd.Context(), func(ctx context.Context, c *telegram.Client) error {
				files, err := c.FileService.GetFilesFromNewMessages(ctx, ID)
				if err != nil {
					return err
				}

				downloader := r.getDownloader(ctx)
				downloader.Start(ctx)
				downloader.RunAsyncDownloader(ctx, files)
				return downloader.Stop(ctx)
			}))
		},
	}

	downloadCmd.AddCommand(downloadHistoryCmd)
	downloadCmd.AddCommand(downloadWatcherCmd)

	userCmd.AddCommand(downloadCmd)
	return userCmd
}
