package cmd

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func renderPeerTable(chats []telegram.PeerInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"Name",
			"ID",
			"Type",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "Name", Mode: table.Asc},
	})

	for _, chat := range chats {
		t.AppendRow(
			table.Row{
				chat.Name,
				chat.ID,
				chat.Type.String(),
			},
		)
	}

	t.Render()
}

func renderUserTable(users []telegram.UserInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"ID",
			"Username",
			"First Name",
			"Last Name",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "ID", Mode: table.AscNumeric},
	})

	for _, user := range users {
		t.AppendRow(
			table.Row{
				user.ID,
				user.Username,
				user.FirstName,
				user.LastName,
			},
		)
	}

	t.Render()
}

func renderUser(user telegram.UserInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(
		table.Row{
			"ID",
			"Username",
			"First Name",
			"Last Name",
		},
	)

	t.AppendRow(
		table.Row{
			user.ID,
			user.Username,
			user.FirstName,
			user.LastName,
		},
	)

	t.Render()
}

func renderUserTableAsync(ctx context.Context, u <-chan telegram.UserInfo, total int) error {
	pw := progress.NewWriter()
	pw.SetAutoStop(true)
	pw.SetTrackerLength(25)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetSortBy(progress.SortByPercentDsc)
	pw.SetStyle(progress.StyleDefault)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Visibility.ETA = true
	pw.Style().Visibility.ETAOverall = true

	go pw.Render()

	tracker := &progress.Tracker{
		Total:   int64(total),
		Message: "Fetching users",
		Units:   progress.UnitsDefault,
	}

	pw.AppendTracker(tracker)
	var users []telegram.UserInfo

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		renderUserTable(users)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case user, ok := <-u:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				users = append(users, user)
			}
		}
	})

	if err := g.Wait(); err != nil {
		tracker.MarkAsErrored()
		return err
	}

	tracker.MarkAsDone()
	return nil
}

func newPeerCmd(ctx context.Context, r *Root) *cobra.Command {
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
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				peers, err := c.GetAllPeers(ctx)
				if err != nil {
					return err
				}

				renderPeerTable(peers)
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
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				peer, err := c.FindPeer(ctx, args[0])
				if err != nil {
					return err
				}

				switch peer.Type {
				case telegram.PeerTypeUser:
					user, err := c.GetUser(ctx, peer.ID)
					if err != nil {
						return err
					}

					renderUser(user)

				case telegram.PeerTypeChat:
					fallthrough
				case telegram.PeerTypeChannel:
					renderPeerTable([]telegram.PeerInfo{peer})
				}
				return nil
			})
		},
	}

	peer.AddCommand(peerListCmd)
	peer.AddCommand(peerFindCmd)
	return peer
}

func newChatCmd(ctx context.Context, r *Root) *cobra.Command {
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

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, total, err := c.GetAllUsersFromChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				return renderUserTableAsync(ctx, users, total)
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

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, err := c.GetUsersFromChat(ctx, chatID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	userFindCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	userFindCmd.MarkFlagRequired("user")

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)

	chat.AddCommand(userCmd)
	return chat
}

func newChannelCmd(ctx context.Context, r *Root) *cobra.Command {
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

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				u, total, err := c.GetAllUsersFromChannel(ctx, channelID)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				return renderUserTableAsync(ctx, u, total)
			})
		},
	}

	userFromHistoryCmd := &cobra.Command{
		Use:   "from-history",
		Short: "List all users in a channel from its message history",
		Long:  `List all users in a channel from its message history.`,
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

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				usersChan, errors := c.GetUsersFromMessageHistory(ctx, chatID, userQuery)
				g, ctx := errgroup.WithContext(ctx)
				g.Go(func() error {
					for {
						select {
						case <-ctx.Done():
							return ctx.Err()

						case err, ok := <-errors:
							if !ok {
								return nil
							}
							r.log.Error("failed to get user", zap.Error(err))

						case user, ok := <-usersChan:
							if !ok {
								return nil
							}

							renderUser(user)
						}
					}
				})

				return g.Wait()
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

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				users, err := c.GetUsersFromChannel(ctx, channelID, userQuery)
				if err != nil {
					r.log.Error("failed to get users", zap.Error(err))
					return err
				}

				renderUserTable(users)
				return nil
			})
		},
	}

	userFindCmd.Flags().StringP("user", "u", "", "Username/first name/last name of the user to find")
	userFindCmd.MarkFlagRequired("user")

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userFindCmd)
	userCmd.AddCommand(userFromHistoryCmd)

	channel.AddCommand(userCmd)
	return channel
}
