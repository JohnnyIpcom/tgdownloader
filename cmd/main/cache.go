package cmd

import (
	"context"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/cobra"
)

func (r *Root) newCacheCmd() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "сache",
		Short: "Manage cache",
		Long:  "Manage cache",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	cacheViewCmd := &cobra.Command{
		Use:   "view",
		Short: "view cache",
		Long:  "view cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(cmd.Context(), func(ctx context.Context, client *telegram.Client) error {
				cachedPeers, err := r.client.CacheService.GetPeersFromCache(ctx)
				if err != nil {
					return err
				}

				return renderer.RenderCachedPeerTableAsync(ctx, cachedPeers)
			})
		},
	}

	cacheUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "update cache",
		Long:  "update cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(cmd.Context(), func(ctx context.Context, client *telegram.Client) error {
				return r.client.CacheService.UpdateDialogCache(ctx)
			})
		},
	}

	cacheCmd.AddCommand(cacheViewCmd)
	cacheCmd.AddCommand(cacheUpdateCmd)
	return cacheCmd
}
