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
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := cmd.Flags().GetString("type")
			if err != nil {
				return err
			}

			var filterFuncs []telegram.CachedPeerFilter
			if kind != "" {
				switch kind {
				case "user":
					filterFuncs = append(filterFuncs, telegram.OnlyUsersCachedPeerFilter())
				case "chat":
					filterFuncs = append(filterFuncs, telegram.OnlyChatsCachedPeerFilter())
				case "channel":
					filterFuncs = append(filterFuncs, telegram.OnlyChannelsCachedPeerFilter())
				}
			}

			name, err := cmd.Flags().GetString("name")
			if err != nil {
				return err
			}

			if name != "" {
				filterFuncs = append(filterFuncs, telegram.NameCachedPeerFilter(name))
			}

			cachedPeers, err := r.client.CacheService.GetCachedPeers(cmd.Context(), filterFuncs...)
			if err != nil {
				return err
			}

			renderer.RenderCachedPeerTable(cachedPeers)
			return nil
		},
	}

	cacheViewCmd.Flags().StringP("type", "t", "", "filter by type")
	cacheViewCmd.Flags().StringP("name", "n", "", "filter by name")

	cacheCmd.AddCommand(cacheViewCmd)
	return cacheCmd
}
