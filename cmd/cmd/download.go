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
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			tdLibPeerID, err := r.parseTDLibPeerID(args[0])
			if err != nil {
				r.log.Error(err, "failed to convert user ID")
				return err
			}

			peer, err := r.client.PeerService.ResolveTDLibID(cmd.Context(), tdLibPeerID)
			if err != nil {
				r.log.Error(err, "failed to resolve peer")
				return err
			}

			return r.downloadFilesFromPeer(cmd.Context(), peer, opts)
		},
	}

	downloadHistoryCmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	downloadHistoryCmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	downloadHistoryCmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	downloadHistoryCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadHistoryCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadHistoryCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")

	downloadWatcherCmd := &cobra.Command{
		Use:   "watcher",
		Short: "Watch a peer for new files",
		Long:  `Watch a peer for new files.`,
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"prompt_suggest": "any",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			tdLibPeerID, err := r.parseTDLibPeerID(args[0])
			if err != nil {
				r.log.Error(err, "failed to convert user ID")
				return err
			}

			peer, err := r.client.PeerService.ResolveTDLibID(cmd.Context(), tdLibPeerID)
			if err != nil {
				r.log.Error(err, "failed to resolve peer")
				return err
			}

			return r.downloadFilesFromNewMessages(cmd.Context(), peer, opts)
		},
	}

	downloadWatcherCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadWatcherCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadWatcherCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")

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

			return r.downloadFilesFromMessage(cmd.Context(), peer, msgId, opts)
		},
	}

	downloadMessageCmd.Flags().BoolVar(&opts.single, "single", false, "Download only one file")
	downloadMessageCmd.Flags().BoolVar(&opts.hashtags, "hashtags", false, "Save hashtags as folders")
	downloadMessageCmd.Flags().BoolVar(&opts.rewrite, "rewrite", false, "Rewrite files if they already exist")
	downloadMessageCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Do not download files, just print what would be downloaded")

	downloadCmd.AddCommand(
		downloadHistoryCmd,
		downloadWatcherCmd,
		downloadMessageCmd,
	)

	r.setupConnectionForCmd(
		downloadHistoryCmd,
		downloadWatcherCmd,
		downloadMessageCmd,
	)
	return downloadCmd
}
