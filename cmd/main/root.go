package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
)

// Root is the root command for the application.
type Root struct {
	cfgPath string
	version string

	cfg    config.Config
	client *telegram.Client
}

// NewRoot creates a new root command.
func NewRoot(version string) (*Root, error) {
	root := &Root{
		version: version,
		cfg:     viper.NewConfig(),
	}

	cobra.OnInitialize(
		root.loadConfig,
		root.initClient,
	)

	return root, nil
}

// Execute executes the root command.
func (r *Root) Execute(ctx context.Context) error {
	rootCmd := &cobra.Command{
		Use:   "tgdownloader",
		Short: "Telegram CLI Downloader",
		Long:  "Telegram CLI Downloader is a CLI tool to download Telegram files from a chat, channel or user, even if this chat, channel or user is not in private mode.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	rootCmd.PersistentFlags().StringVarP(
		&r.cfgPath,
		"config",
		"c",
		"",
		"config file (default \"$HOME/.tgdownloader\")",
	)

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "print version info",
		Long:  "print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Telegram CLI Downloader v%s\n", r.version)
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(newChannelsCmd(ctx, r))
	rootCmd.AddCommand(newDownloadCmd(ctx, r))

	return rootCmd.Execute()
}

func (r *Root) loadConfig() {
	if err := r.cfg.Load("tgdownloader", r.cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (r *Root) initClient() {
	client, err := telegram.NewClient(r.cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r.client = client
}
