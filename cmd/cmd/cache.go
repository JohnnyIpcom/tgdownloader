package cmd

import (
	"github.com/johnnyipcom/tgdownloader/internal/renderer"

	"github.com/spf13/cobra"
)

func (r *Root) newCacheCmd() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
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
			cachedPeers, err := r.client.CacheService.GetPeersFromCache(cmd.Context())
			if err != nil {
				return err
			}

			return renderer.RenderCachedPeerTableAsync(cmd.Context(), cachedPeers)
		},
	}

	cacheUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "update cache",
		Long:  "update cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.CacheService.UpdateDialogCache(cmd.Context())
		},
	}

	cacheCmd.AddCommand(cacheViewCmd)
	cacheCmd.AddCommand(cacheUpdateCmd)
	return cacheCmd
}
