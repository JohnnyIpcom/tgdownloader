package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/johnnyipcom/tgdownloader/internal/dwpool"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"github.com/johnnyipcom/tgdownloader/pkg/dropbox"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	cc "github.com/ivanpirog/coloredcobra"
)

// Root is the root command for the application.
type Root struct {
	cfgPath   string
	version   string
	verbosity string

	cfg    config.Config
	client *telegram.Client
	log    *zap.Logger
	level  zap.AtomicLevel

	loader *dwpool.Downloader
	ldOnce sync.Once
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

	cobra.OnFinalize(
		root.syncLogger,
	)

	return root, nil
}

// newVersionCmd creates a command to print the version.
func (r *Root) newVersionCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "print version info",
		Long:  "print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Telegram CLI Downloader v%s\n", r.version)
		},
	}

	return versionCmd
}

// getRootCmd returns the root command.
func (r *Root) getRootCmd() *cobra.Command {
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

	rootCmd.PersistentFlags().StringVarP(
		&r.verbosity,
		"verbosity",
		"v",
		"info",
		"verbosity level (debug, info, warn, error, fatal, panic)",
	)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cmd.SetContext(ctxlogger.WithLogger(cmd.Context(), r.log))
		level, err := zap.ParseAtomicLevel(r.verbosity)
		if err != nil {
			return err
		}

		r.level.SetLevel(level.Level())
		return nil
	}

	rootCmd.AddCommand(r.newVersionCmd())
	rootCmd.AddCommand(r.newPeerCmd())
	rootCmd.AddCommand(r.newChatCmd())
	rootCmd.AddCommand(r.newChannelCmd())
	rootCmd.AddCommand(r.newUserCmd())
	rootCmd.AddCommand(r.newDialogsCmd())
	rootCmd.AddCommand(r.newCacheCmd())

	return rootCmd
}

// ExecuteContext executes the root command with the given context.
func (r *Root) ExecuteContext(ctx context.Context) error {
	rootCmd := r.getRootCmd()

	cc.Init(&cc.Config{
		RootCmd:  rootCmd,
		Headings: cc.HiCyan + cc.Bold + cc.Underline,
		Commands: cc.HiYellow + cc.Bold,
		Example:  cc.Italic,
		ExecName: cc.Bold,
		Flags:    cc.Bold,
	})

	return rootCmd.ExecuteContext(ctx)
}

func (r *Root) loadConfig() {
	if err := r.cfg.Load("tgdownloader", r.cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (r *Root) initClient() {
	client, err := telegram.NewClient(r.cfg.Sub("telegram"), r.log.Named("telegram"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r.client = client
}

func (r *Root) initLogger() {
	zapConfig := zap.NewDevelopmentConfig()
	if err := r.cfg.Sub("logger").Unmarshal(&zapConfig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	enc := zap.NewDevelopmentEncoderConfig()
	enc.EncodeLevel = zapcore.CapitalColorLevelEncoder

	zapConfig.Level = r.level
	zapConfig.EncoderConfig = enc

	log, err := zapConfig.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	r.log = log
}

func (r *Root) syncLogger() {
	_ = r.log.Sync()
	fmt.Println("Logger synced")
}

func (r *Root) getDownloader(ctx context.Context) *dwpool.Downloader {
	r.ldOnce.Do(func() {
		var fs afero.Fs
		switch strings.ToLower(r.cfg.GetString("downloader.type")) {
		case "local":
			fs = afero.NewOsFs()

		case "dropbox":
			dropbox, err := dropbox.NewFs(ctx, r.cfg.Sub("downloader.dropbox"), r.log.Named("dropbox"))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			fs = dropbox

		default:
			fmt.Fprintln(os.Stderr, "Unknown file system type")
			os.Exit(1)
		}

		r.loader = dwpool.NewDownloader(fs, r.client.FileService, 5)
		r.loader.SetOutputDir(r.cfg.GetString("downloader.dir.output"))
	})

	return r.loader
}
