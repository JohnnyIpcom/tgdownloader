package cmd

import (
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

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
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filterFuncs := []telegram.CachedPeerFilter{}
			if len(args) > 0 {
				filterFuncs = append(filterFuncs, telegram.NameCachedPeerFilter(args[0]))
			}

			cachedPeers, err := r.client.CacheService.GetCachedPeers(cmd.Context(), filterFuncs...)
			if err != nil {
				return err
			}

			renderer.RenderCachedPeerTable(cachedPeers)
			return nil
		},
	}

	cacheCmd.AddCommand(cacheViewCmd)

	r.setupConnectionForCmd(cacheViewCmd)
	return cacheCmd
}
