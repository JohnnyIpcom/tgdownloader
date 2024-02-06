package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/johnnyipcom/tgdownloader/cmd/version"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	cc "github.com/ivanpirog/coloredcobra"

	"github.com/spf13/cobra"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Root is the root command for the application.
type Root struct {
	version   string
	verbosity string

	cfg    config.Config
	client *telegram.Client
	stop   telegram.StopFunc
	zap    *zap.Logger
	log    logr.Logger
	level  zap.AtomicLevel
}

// NewRoot creates a new root command.
func NewRoot(version string) (*Root, error) {
	cfg := viper.NewConfig()
	if err := cfg.Load("tgdownloader", ""); err != nil {
		return nil, err
	}

	zapConfig := zap.NewDevelopmentConfig()
	if err := cfg.Sub("logger").Unmarshal(&zapConfig); err != nil {
		return nil, err
	}

	enc := zap.NewDevelopmentEncoderConfig()
	enc.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapConfig.EncoderConfig = enc

	level := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	zapConfig.Level = level

	zap, err := zapConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, err
	}

	client, err := telegram.NewClient(cfg.Sub("telegram"), zap.Named("telegram"))
	if err != nil {
		return nil, err
	}

	return &Root{
		version: version,
		cfg:     cfg,
		client:  client,
		zap:     zap,
		log:     zapr.NewLogger(zap),
		level:   level,
	}, nil
}

// newVersionCmd creates a command to print the version.
func (r *Root) newVersionCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Long:  "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Telegram CLI Downloader %s\n", r.version)
		},
	}

	return versionCmd
}

// newRootCmd returns the root command.
func (r *Root) newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tgdownloader",
		Short:         "Telegram CLI Downloader",
		Long:          "Telegram CLI Downloader is a CLI tool to download Telegram files from a chat, channel or user, even if this chat, channel or user is not in private mode.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	rootCmd.PersistentFlags().StringVarP(
		&r.verbosity,
		"verbosity",
		"v",
		"debug",
		"verbosity level (debug, info, warn, error, fatal, panic)",
	)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-stop
			cancel()
		}()

		cmd.SetContext(logr.NewContext(ctx, r.log))
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

	// Prompt command must be the last one to initialize all other commands first.
	rootCmd.AddCommand(r.newPromptCmd(rootCmd))
	return rootCmd
}

func (r *Root) Execute() error {
	rootCmd := r.newRootCmd()

	cc.Init(&cc.Config{
		RootCmd:  rootCmd,
		Headings: cc.HiCyan + cc.Bold + cc.Underline,
		Commands: cc.HiYellow + cc.Bold,
		Example:  cc.Italic,
		ExecName: cc.Bold,
		Flags:    cc.Bold,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := r.client.Connect(ctx)
	if err != nil {
		return err
	}

	defer func() {
		stop()
		renderer.RenderBye()
	}()

	r.stop = stop
	return rootCmd.ExecuteContext(ctx)
}

func (r *Root) newDownloader() (*downloader.Downloader, error) {
	dCfg := r.cfg.Sub("downloader")

	workers := dCfg.GetInt("workers")
	if workers < 1 {
		workers = runtime.NumCPU()
	}

	loader := downloader.NewDownloader(
		downloader.GetFS(dCfg, zap.NewStdLog(r.zap)),
		workers,
		r.client.FileService,
	)

	loader.SetOutputDir(r.cfg.GetString("downloader.dir.output"))
	return loader, nil
}

func Run() {
	root, err := NewRoot(version.Version())
	if err != nil {
		renderer.RenderError(err)
		return
	}

	renderer.RenderError(root.Execute())
}
