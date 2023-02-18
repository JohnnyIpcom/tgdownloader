package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// Root is the root command for the application.
type Root struct {
	cfgPath   string
	logPath   string
	version   string
	verbosity string

	cfg    config.Config
	client telegram.Client
	log    *zap.Logger
	level  zap.AtomicLevel
}

// NewRoot creates a new root command.
func NewRoot(version string) (*Root, error) {
	root := &Root{
		version: version,
		cfg:     viper.NewConfig(),
		level:   zap.NewAtomicLevelAt(zap.ErrorLevel),
	}

	cobra.OnInitialize(
		root.loadConfig,
		root.initLogger,
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

	rootCmd.PersistentFlags().StringVar(
		&r.logPath,
		"log",
		"",
		"log file (default \"stderr\")",
	)

	rootCmd.PersistentFlags().StringVarP(
		&r.verbosity,
		"verbosity",
		"v",
		"info",
		"verbosity level (debug, info, warn, error, fatal, panic)",
	)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		level, err := zap.ParseAtomicLevel(r.verbosity)
		if err != nil {
			return err
		}

		r.level.SetLevel(level.Level())
		return nil
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "print version info",
		Long:  "print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Telegram CLI Downloader v%s\n", r.version)
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(newPeerCmd(ctx, r))
	rootCmd.AddCommand(newChatCmd(ctx, r))
	rootCmd.AddCommand(newChannelCmd(ctx, r))
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
	client, err := telegram.NewClient(r.cfg, r.log)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r.client = client
}

func (r *Root) initLogger() {
	zapConfig := zap.NewDevelopmentConfig()
	logCfg := r.cfg.Sub("logger")
	if err := logCfg.Unmarshal(&zapConfig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if r.logPath != "" {
		zapConfig.OutputPaths = []string{r.logPath}
	}

	zapConfig.Level = r.level

	log, err := zapConfig.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r.log = log
}
