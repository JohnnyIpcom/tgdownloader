package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"

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

	cfg      config.Config
	client   *telegram.Client
	progress renderer.Progress
	zap      *zap.Logger
	log      logr.Logger
	level    zap.AtomicLevel

	runtimeMu           sync.Mutex
	runtimeInitialized  bool
	runtimeErr          error
	promptRootFactory   func() *cobra.Command
	promptGateOnce      sync.Once
	promptGate          chan struct{}
	promptProgramRunner func(*promptModel) error
	promptLogs          *promptLogRouter
	promptRunID         atomic.Uint64
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
	return &Root{version: version}, nil
}

func (r *Root) initializeRuntime() error {
	r.runtimeMu.Lock()
	defer r.runtimeMu.Unlock()
	if r.runtimeInitialized || r.runtimeErr != nil {
		return r.runtimeErr
	}

	cfg := viper.NewConfig()
	if err := cfg.Load("tgdownloader", ""); err != nil {
		r.runtimeErr = err
		return err
	}

	zapConfig := zap.NewDevelopmentConfig()
	if err := cfg.Sub("logger").Unmarshal(&zapConfig); err != nil {
		r.runtimeErr = err
		return err
	}

	enc := zap.NewDevelopmentEncoderConfig()
	enc.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapConfig.EncoderConfig = enc

	level := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	requestedLevel, err := zap.ParseAtomicLevel(r.verbosity)
	if err != nil {
		r.runtimeErr = err
		return err
	}
	level.SetLevel(requestedLevel.Level())
	zapConfig.Level = level

	promptLogs := newPromptLogRouter()
	runtimeZap, err := buildPromptLogger(zapConfig, promptLogs)
	if err != nil {
		r.runtimeErr = err
		return err
	}

	client, err := telegram.NewClient(cfg.Sub("telegram"), runtimeZap.Named("telegram"))
	if err != nil {
		_ = runtimeZap.Sync()
		r.runtimeErr = err
		return err
	}

	progress := renderer.NewProgressWithoutValue()
	client.SetProgress(&progressAdapter{progress})
	r.cfg = cfg
	r.client = client
	r.progress = progress
	r.zap = runtimeZap
	r.log = zapr.NewLogger(runtimeZap)
	r.level = level
	r.promptLogs = promptLogs
	r.runtimeInitialized = true
	return nil
}

// newVersionCmd creates a command to print the version.
func (r *Root) newVersionCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Long:  "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Telegram CLI Downloader %s\n", r.version)
		},
	}

	return versionCmd
}

// newRootCmd returns the root command.
func (r *Root) newRootCmd() *cobra.Command {
	return r.newRootCmdWithPrompt(true)
}

func (r *Root) newPromptRootCmd() *cobra.Command {
	if r.promptRootFactory != nil {
		return r.promptRootFactory()
	}
	return r.newRootCmdWithPrompt(false)
}

func (r *Root) promptCommandGate() chan struct{} {
	r.promptGateOnce.Do(func() {
		r.promptGate = make(chan struct{}, 1)
		r.promptGate <- struct{}{}
	})
	return r.promptGate
}

func (r *Root) newRootCmdWithPrompt(includePrompt bool) *cobra.Command {
	verbosity := r.verbosity
	if strings.TrimSpace(verbosity) == "" {
		verbosity = "debug"
	}
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
		&verbosity,
		"verbosity",
		"v",
		verbosity,
		"verbosity level (debug, info, warn, error, fatal, panic)",
	)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		level, err := zap.ParseAtomicLevel(verbosity)
		if err != nil {
			return err
		}
		r.verbosity = verbosity

		if r.runtimeInitialized {
			cmd.SetContext(logr.NewContext(cmd.Context(), r.log))
			r.level.SetLevel(level.Level())
		}
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
	rootCmd.AddCommand(r.newDownloadCmd())
	rootCmd.AddCommand(r.newExitCmd())

	if includePrompt {
		// Prompt command must be the last one to initialize all other commands first.
		promptCmd := r.newPromptCmd()
		r.setupRuntimeForCmd(promptCmd)
		rootCmd.AddCommand(promptCmd)
	}
	return rootCmd
}

func (r *Root) Execute() error {
	rootCmd := r.newRootCmd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rootCmd.SetContext(ctx)

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
	if r.client != nil {
		if err := r.client.Close(); err != nil {
			return err
		}
	}

	if !r.runtimeInitialized {
		return nil
	}

	r.runtimeInitialized = false
	runtimeZap := r.zap
	if runtimeZap == nil {
		return nil
	}

	//r.progress.Stop()

	renderer.RenderBye(os.Stdout)
	if err := runtimeZap.Sync(); err != nil {
		return err
	}
	return nil
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
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations["runtime"] = "requires_connection"
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			if err := r.initializeRuntime(); err != nil {
				return err
			}
			cmd.SetContext(logr.NewContext(cmd.Context(), r.log))

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

func (r *Root) setupRuntimeForCmd(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations["runtime"] = "runtime_only"
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			if err := r.initializeRuntime(); err != nil {
				return err
			}
			cmd.SetContext(logr.NewContext(cmd.Context(), r.log))
			return nil
		}
	}
}

func (r *Root) newDownloader(ctx context.Context, writer io.Writer, opts ...downloader.Option) (*downloader.Downloader, error) {
	dCfg := r.cfg.Sub("downloader")

	workers := dCfg.GetInt("workers")
	if workers > 1 {
		opts = append(opts, downloader.WithNumWorkers(workers))
	}

	retryCount := dCfg.GetInt("retry.count")
	retryDelay := dCfg.GetDuration("retry.delay")
	if retryCount > 0 || retryDelay > 0 {
		opts = append(opts, downloader.WithRetry(retryCount, retryDelay))
	}

	fs, err := downloader.GetFS(ctx, dCfg, zap.NewStdLog(r.zap), writer)
	if err != nil {
		return nil, err
	}

	loader := downloader.New(
		fs,
		r.client.FileService,
		opts...,
	)

	loader.SetOutputDir(r.cfg.GetString("downloader.dir.output"))
	return loader, nil
}

func Run() {
	root, err := NewRoot(version.Version())
	if err != nil {
		renderer.RenderError(os.Stdout, err)
		return
	}

	renderer.RenderError(os.Stdout, root.Execute())
	type contextCleanupKey struct{}
}
