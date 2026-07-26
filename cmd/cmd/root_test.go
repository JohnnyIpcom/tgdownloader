package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	configviper "github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"go.uber.org/zap"
)

func TestVersionDoesNotInitializeRuntime(t *testing.T) {
	r, err := NewRoot("test-version")
	if err != nil {
		t.Fatalf("new root: %v", err)
	}
	root := r.newRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if r.runtimeInitialized || r.cfg != nil || r.client != nil {
		t.Fatal("version initialized runtime services")
	}
}

func TestHelpDoesNotInitializeRuntime(t *testing.T) {
	r, err := NewRoot("test-version")
	if err != nil {
		t.Fatalf("new root: %v", err)
	}
	root := r.newRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	if !strings.Contains(output.String(), "Telegram CLI Downloader") {
		t.Fatalf("unexpected help output: %q", output.String())
	}
	if r.runtimeInitialized || r.cfg != nil || r.client != nil {
		t.Fatal("help initialized runtime services")
	}
}

func TestStatusFlagKeepsPSAlias(t *testing.T) {
	r := &Root{}
	root := r.newPromptRootCmd()
	cmd, _, err := root.Find([]string{"download", "history"})
	if err != nil {
		t.Fatalf("find download history: %v", err)
	}
	if cmd.Flags().Lookup("status") == nil || cmd.Flags().Lookup("ps") == nil {
		t.Fatal("download history must expose --status and --ps")
	}
}

func TestPromptCommandTreeDoesNotResetSharedVerbosity(t *testing.T) {
	r := &Root{verbosity: "error"}
	root := r.newPromptRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute prompt command tree: %v", err)
	}

	if r.verbosity != "error" {
		t.Fatalf("shared verbosity = %q, want error", r.verbosity)
	}
}

func TestRuntimeEventSinkOptionIsAppliedBeforeClientSetup(t *testing.T) {
	sink := renderer.DiscardEvents()
	options := runtimeInitOptions{}
	withRuntimeEventSink(sink)(&options)
	if options.eventSink != sink {
		t.Fatal("runtime event sink option was not retained")
	}
}

func TestNewDownloaderCancelsDropboxOAuthThroughPromptContext(t *testing.T) {
	cfg := configviper.NewConfig()
	cfg.Set("downloader.type", "dropbox")
	cfg.Set("downloader.dropbox.port", 0)
	cfg.Set("downloader.dropbox.oauth2.id", "test-id")
	cfg.Set("downloader.dropbox.oauth2.secret", "test-secret")
	r := &Root{cfg: cfg, zap: zap.NewNop()}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan renderer.Event, 1)
	done := make(chan error, 1)
	go func() {
		_, err := r.newDownloader(ctx, renderer.NewEventWriter(renderer.NewChannelEventSink(events)))
		done <- err
	}()

	select {
	case event := <-events:
		if event.Kind != renderer.EventLine || event.Text == "" {
			t.Fatalf("Dropbox OAuth event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("Dropbox OAuth instruction was not routed to prompt events")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("newDownloader error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Dropbox OAuth initialization ignored prompt cancellation")
	}
}
