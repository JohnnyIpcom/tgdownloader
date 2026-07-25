package cmd

import (
	"github.com/spf13/cobra"
)

func (r *Root) newDownloadCmd() *cobra.Command {
	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a peer",
		Long:  `Download files from chat, channel or user`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
	}

	var opts downloadOptions
	downloadHistoryCmd := &cobra.Command{
		Use:   "history",
		Short: "Download files from a peer history",
		Long:  `Download files from a chat, channel or user history.`,
		Example: `  tgdownloader download history "Cherry Channel"
  tgdownloader download history 0xFFFFFF000000007B --limit 25`,
		Args: peerInputArgs,
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.resolvePeer(cmd.Context(), peerInputArg(args))
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			return r.downloadFilesFromPeer(cmd.Context(), cmd.OutOrStdout(), peer, opts)
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	downloadHistoryCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadHistoryCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadHistoryCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")
	addStatusFlags(downloadHistoryCmd, &opts.ps)

	downloadWatcherCmd := &cobra.Command{
		Use:     "watcher",
		Short:   "Watch a peer for new files",
		Long:    `Watch a peer for new files.`,
		Example: `  tgdownloader download watcher "Cherry Channel" --status`,
		Args:    peerInputArgs,
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.resolvePeer(cmd.Context(), peerInputArg(args))
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			return r.downloadFilesFromNewMessages(cmd.Context(), cmd.OutOrStdout(), peer, opts)
		},
	}

	downloadWatcherCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadWatcherCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadWatcherCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")
	addStatusFlags(downloadWatcherCmd, &opts.ps)

	downloadMessageCmd := &cobra.Command{
		Use:   "message",
		Short: "Download a file from a message",
		Long:  `Download a file from a message.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, msgId, err := r.client.ParseMessageLink(cmd.Context(), args[0])
			if err != nil {
				r.log.Error(err, "failed to parse message link")
				return err
			}

			return r.downloadFilesFromMessage(cmd.Context(), cmd.OutOrStdout(), peer, msgId, opts)
		},
	}

	downloadMessageCmd.Flags().BoolVar(&opts.single, "single", false, "Download only one file")
	downloadMessageCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadMessageCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadMessageCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")
	addStatusFlags(downloadMessageCmd, &opts.ps)

	downloadYandexDiskCmd := &cobra.Command{
		Use:   "yadisk",
		Short: "Download files from Yandex Disk links in peer history",
		Long:  `Download files from Yandex Disk links found in message text of chat, channel or user history.`,
		Args:  peerInputArgs,
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			peer, err := r.resolvePeer(cmd.Context(), peerInputArg(args))
			if err != nil {
				r.log.Error(err, "failed to parse peer")
				return err
			}

			return r.downloadYandexDiskFromPeer(cmd.Context(), cmd.OutOrStdout(), peer, opts)
		},
	}

	downloadYandexDiskCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of links to download")
	downloadYandexDiskCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadYandexDiskCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	downloadYandexDiskCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadYandexDiskCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadYandexDiskCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")
	addStatusFlags(downloadYandexDiskCmd, &opts.ps)

	downloadCmd.AddCommand(
		downloadHistoryCmd,
		downloadWatcherCmd,
		downloadMessageCmd,
		downloadYandexDiskCmd,
	)

	r.setupConnectionForCmd(
		downloadHistoryCmd,
		downloadWatcherCmd,
		downloadMessageCmd,
		downloadYandexDiskCmd,
	)
	return downloadCmd
}

func addStatusFlags(cmd *cobra.Command, enabled *bool) {
	cmd.Flags().BoolVar(enabled, "status", false, "Enable status information")
	cmd.Flags().BoolVar(enabled, "ps", false, "Enable status information")
	_ = cmd.Flags().MarkDeprecated("ps", "use --status")
}
