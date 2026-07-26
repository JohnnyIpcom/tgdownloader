package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	runtimeModeOnly               = "runtime_only"
	runtimeModeRequiresConnection = "requires_connection"
)

type runtimeCommandRequest struct {
	Mode string
	Args []string
}

type presentedRuntimeError struct {
	err error
}

func (e *presentedRuntimeError) Error() string {
	return e.err.Error()
}

func (e *presentedRuntimeError) Unwrap() error {
	return e.err
}

func renderUnpresentedError(writer io.Writer, err error) {
	var presented *presentedRuntimeError
	if errors.As(err, &presented) {
		return
	}

	renderer.RenderError(writer, err)
}

func (r *Root) routeRuntimeCommands(root *cobra.Command) {
	for _, command := range root.Commands() {
		r.routeRuntimeCommand(command)
	}
}

func (r *Root) routeRuntimeCommand(command *cobra.Command) {
	for _, child := range command.Commands() {
		r.routeRuntimeCommand(child)
	}

	mode := command.Annotations["runtime"]
	if mode == "" || command.Name() == "prompt" {
		return
	}

	command.Run = nil
	command.RunE = func(cmd *cobra.Command, args []string) error {
		request := runtimeCommandRequest{
			Mode: mode,
			Args: runtimeCommandArgs(cmd, args),
		}

		if r.runtimeCommandRunner != nil {
			return r.runtimeCommandRunner(cmd.Context(), request)
		}

		return r.runOneShotTUI(cmd.Context(), request)
	}
}

func runtimeCommandArgs(command *cobra.Command, positional []string) []string {
	var path []string
	for current := command; current.Parent() != nil; current = current.Parent() {
		path = append([]string{current.Name()}, path...)
	}

	args := append([]string(nil), path...)
	command.Flags().Visit(func(flag *pflag.Flag) {
		args = append(args, fmt.Sprintf("--%s=%s", flag.Name, flag.Value.String()))
	})

	return append(args, positional...)
}

func (r *Root) runOneShotTUI(ctx context.Context, request runtimeCommandRequest) (runErr error) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())

	events := make(chan renderer.Event, promptRendererEventBufferSize)
	sink := renderer.NewContextChannelEventSink(lifetime, events)
	if r.promptLogs != nil {
		r.promptLogs.SetSink(sink)
	}

	provider := newTUIAuthCodeProvider(lifetime)
	startupCtx, cancelStartup := context.WithCancel(ctx)
	startupDone := make(chan struct{})

	var startupClaimed atomic.Bool
	startup := func() tea.Msg {
		if !startupClaimed.CompareAndSwap(false, true) {
			return oneShotStartupDoneMsg{Err: context.Canceled}
		}

		defer close(startupDone)

		return oneShotStartupDoneMsg{Err: r.startOneShotRuntime(startupCtx, sink, provider, request.Mode)}
	}

	var commandDone <-chan struct{}
	model := newOneShotModel(oneShotModelOptions{
		Context:       ctx,
		Lifetime:      lifetime,
		Events:        events,
		Startup:       startup,
		StartupCancel: cancelStartup,
		AuthRequests:  provider.Requests(),
		Execute: func(commandCtx context.Context) tea.Cmd {
			done := make(chan struct{})
			commandDone = done
			command := r.submitRuntimeCommand(commandCtx, request.Args, sink)

			return func() tea.Msg {
				defer close(done)
				return command()
			}
		},
	})

	runErr = r.runOneShotProgram(model)

	cancelStartup()
	if model.commandCancel != nil {
		model.commandCancel()
	}
	if startupClaimed.CompareAndSwap(false, true) {
		close(startupDone)
	}

	waitForOneShotWork(startupDone, commandDone, events)
	cancelLifetime()

	if r.promptLogs != nil {
		r.promptLogs.SetSink(nil)
	}

	programErr := runErr
	closeErr := r.Close()
	if programErr != nil {
		renderer.RenderError(os.Stdout, programErr)
	}
	if closeErr != nil {
		renderer.RenderError(os.Stdout, closeErr)
	}

	runErr = errors.Join(programErr, model.startupErr, model.commandErr, closeErr)
	if runErr != nil {
		return &presentedRuntimeError{err: runErr}
	}

	return nil
}

func (r *Root) runOneShotProgram(model *oneShotModel) error {
	if r.oneShotProgramRunner != nil {
		return r.oneShotProgramRunner(model)
	}

	_, err := tea.NewProgram(model).Run()
	return err
}

func waitForOneShotWork(startupDone, commandDone <-chan struct{}, events <-chan renderer.Event) {
	for startupDone != nil || commandDone != nil {
		select {
		case <-startupDone:
			startupDone = nil
		case <-commandDone:
			commandDone = nil
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		}
	}
}
