package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

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
	"github.com/spf13/pflag"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Root is the root command for the application.
type Root struct {
	version   string
	verbosity string
	stopFunc  telegram.StopFunc
	progress  telegram.Progress

	cfg    config.Config
	client *telegram.Client
	zap    *zap.Logger
	log    logr.Logger
	level  zap.AtomicLevel
}

type progressAdapter struct {
	renderer.Progress
}

var _ telegram.Progress = (*progressAdapter)(nil)

func (p *progressAdapter) Tracker(msg string) telegram.Tracker {
	return p.Progress.UnitsTracker(msg, 0)
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

	progress := &progressAdapter{Progress: renderer.NewProgress()}
	client.SetProgress(progress)
	return &Root{
		version:  version,
		cfg:      cfg,
		client:   client,
		zap:      zap,
		log:      zapr.NewLogger(zap),
		level:    level,
		progress: progress,
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
		SilenceErrors: true,
		SilenceUsage:  true,
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
			signal.Stop(stop)
		}()

		cmd.SetContext(logr.NewContext(ctx, r.log))
		level, err := zap.ParseAtomicLevel(r.verbosity)
		if err != nil {
			return err
		}

		r.level.SetLevel(level.Level())
		return nil
	}

	rootCmd.PersistentPostRunE = func(cmd *cobra.Command, args []string) error {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			f.Value.Set(f.DefValue)
		})
		return nil
	}

	rootCmd.AddCommand(r.newVersionCmd())
	rootCmd.AddCommand(r.newPeerCmd())
	rootCmd.AddCommand(r.newDialogsCmd())
	rootCmd.AddCommand(r.newCacheCmd())
	rootCmd.AddCommand(r.newDownloadCmd())

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

	return rootCmd.Execute()
}

func (r *Root) Close() error {
	r.Disconnect()

	//r.progress.Stop()

	renderer.RenderBye()
	return r.zap.Sync()
}

func (r *Root) IsConnected() bool {
	return r.stopFunc != nil
}

func (r *Root) Connect(ctx context.Context) error {
	if r.IsConnected() {
		return nil
	}

	stop, err := r.client.Connect(ctx)
	if err != nil {
		return err
	}

	r.stopFunc = stop
	return nil
}

func (r *Root) Disconnect() {
	if r.stopFunc != nil {
		r.stopFunc()
		r.stopFunc = nil
	}
}

type needCloseKey struct{}

func (r *Root) setupConnectionForCmd(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			ctx := context.WithValue(cmd.Context(), needCloseKey{}, !r.IsConnected())
			cmd.SetContext(ctx)

			return r.Connect(ctx)
		}

		cmd.PostRunE = func(cmd *cobra.Command, args []string) error {
			if needDisconnect, ok := cmd.Context().Value(needCloseKey{}).(bool); ok && needDisconnect {
				return r.Close()
			}

			return nil
		}
	}
}

func (r *Root) newDownloader(opts ...downloader.Option) (*downloader.Downloader, error) {
	dCfg := r.cfg.Sub("downloader")

	workers := dCfg.GetInt("workers")
	if workers > 1 {
		opts = append(opts, downloader.WithNumWorkers(workers))
	}

	loader := downloader.New(
		downloader.GetFS(dCfg, zap.NewStdLog(r.zap)),
		r.client.FileService,
		opts...,
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
