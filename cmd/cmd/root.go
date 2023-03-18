package cmd

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/johnnyipcom/tgdownloader/internal/dwpool"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"github.com/johnnyipcom/tgdownloader/pkg/dropbox"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"

	cc "github.com/ivanpirog/coloredcobra"

	"github.com/spf13/afero"
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
	log    *zap.Logger
	level  zap.AtomicLevel

	loader *dwpool.Downloader
	ldOnce sync.Once

	cmdRoot *cobra.Command
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

	log, err := zapConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, err
	}

	client, err := telegram.NewClient(cfg.Sub("telegram"), log.Named("telegram"))
	if err != nil {
		return nil, err
	}

	root := &Root{
		version: version,
		cfg:     cfg,
		client:  client,
		log:     log,
		level:   level,
	}

	root.cmdRoot = root.newRootCmd()
	return root, nil
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

	rootCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) {
		r.log.Sync()
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

func (r *Root) ExecuteContext(ctx context.Context) error {
	cc.Init(&cc.Config{
		RootCmd:  r.cmdRoot,
		Headings: cc.HiCyan + cc.Bold + cc.Underline,
		Commands: cc.HiYellow + cc.Bold,
		Example:  cc.Italic,
		ExecName: cc.Bold,
		Flags:    cc.Bold,
	})

	stop, err := r.client.Connect(ctx)
	if err != nil {
		return err
	}

	defer stop()
	if err := r.cmdRoot.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			r.renderBye()
			return nil
		}

		return err
	}

	r.renderBye()
	return nil
}

func (r *Root) getDownloaderFS(ctx context.Context) (afero.Fs, error) {
	switch strings.ToLower(r.cfg.GetString("downloader.type")) {
	case "local":
		return afero.NewOsFs(), nil

	case "dropbox":
		logger, err := zap.NewStdLogAt(r.log, zap.InfoLevel)
		if err != nil {
			return nil, err
		}

		client := <-dropbox.RunOauth2Server(ctx, r.cfg.Sub("downloader.dropbox"), r.log)
		return dropbox.NewFs(ctx, client, logger)
	}

	return nil, fmt.Errorf("invalid downloader type: %s", r.cfg.GetString("downloader.type"))
}

func (r *Root) getDownloader(ctx context.Context) *dwpool.Downloader {
	r.ldOnce.Do(func() {
		fs, err := r.getDownloaderFS(ctx)
		if err != nil {
			panic(err)
		}

		threads := r.cfg.GetInt("downloader.threads")
		if threads <= 0 {
			threads = runtime.NumCPU()
		}

		r.loader = dwpool.NewDownloader(fs, r.client.FileService, threads)
		r.loader.SetOutputDir(r.cfg.GetString("downloader.dir.output"))
	})

	return r.loader
}

func (r *Root) renderBye() {
	renderer.Println(renderer.Colors{renderer.FgCyan}, "Bye! ^_^")
}
