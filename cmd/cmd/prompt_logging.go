package cmd

import (
	"io"
	"strings"
	"sync"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type promptLogRouter struct {
	mu   sync.RWMutex
	sink renderer.EventSink
}

func newPromptLogRouter() *promptLogRouter {
	return &promptLogRouter{}
}

func (r *promptLogRouter) SetSink(sink renderer.EventSink) {
	r.mu.Lock()
	r.sink = sink
	r.mu.Unlock()
}

func (r *promptLogRouter) Sink() renderer.EventSink {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sink
}

type promptRoutingCore struct {
	base   zapcore.Core
	router *promptLogRouter
}

func newPromptRoutingCore(base zapcore.Core, router *promptLogRouter, _ zapcore.Encoder) zapcore.Core {
	return &promptRoutingCore{base: base, router: router}
}

func (c *promptRoutingCore) Enabled(level zapcore.Level) bool {
	return c.base.Enabled(level)
}

func (c *promptRoutingCore) With(fields []zapcore.Field) zapcore.Core {
	return &promptRoutingCore{
		base:   c.base.With(fields),
		router: c.router,
	}
}

func (c *promptRoutingCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}

	return checked
}

func (c *promptRoutingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if c.router.Sink() != nil {
		return nil
	}

	return c.base.Write(entry, fields)
}

func (c *promptRoutingCore) Sync() error {
	return c.base.Sync()
}

func buildPromptLogger(config zap.Config, router *promptLogRouter) (*zap.Logger, error) {
	terminalPaths, retainedPaths := splitZapTerminalPaths(config.OutputPaths)
	if len(terminalPaths) == 0 {
		return config.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	}

	_, retainedErrorPaths := splitZapTerminalPaths(config.ErrorOutputPaths)
	build := func(paths []string) (*zap.Logger, error) {
		cfg := config
		cfg.OutputPaths = append([]string(nil), paths...)
		cfg.ErrorOutputPaths = append([]string(nil), retainedErrorPaths...)

		discardInternalErrors := len(cfg.ErrorOutputPaths) == 0
		if discardInternalErrors {
			cfg.ErrorOutputPaths = []string{"stderr"}
		}

		logger, err := cfg.Build(zap.AddStacktrace(zapcore.ErrorLevel))
		if err != nil {
			return nil, err
		}

		if discardInternalErrors {
			logger = logger.WithOptions(zap.ErrorOutput(zapcore.AddSync(io.Discard)))
		}

		return logger, nil
	}

	terminalLogger, err := build(terminalPaths)
	if err != nil {
		return nil, err
	}

	terminalCore := newPromptRoutingCore(terminalLogger.Core(), router, promptLogEncoder(config))
	if len(retainedPaths) == 0 {
		return terminalLogger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
			return terminalCore
		})), nil
	}

	retainedLogger, err := build(retainedPaths)
	if err != nil {
		return nil, err
	}

	return retainedLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewTee(core, terminalCore)
	})), nil
}

func splitZapTerminalPaths(paths []string) (terminal, retained []string) {
	for _, path := range paths {
		switch strings.ToLower(strings.TrimSpace(path)) {
		case "stdout", "stderr":
			terminal = append(terminal, path)
		default:
			retained = append(retained, path)
		}
	}

	return terminal, retained
}

func promptLogEncoder(config zap.Config) zapcore.Encoder {
	if strings.EqualFold(config.Encoding, "json") {
		return zapcore.NewJSONEncoder(config.EncoderConfig)
	}

	return zapcore.NewConsoleEncoder(config.EncoderConfig)
}
