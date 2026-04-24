package telegram

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	tgclient "github.com/gotd/td/telegram"
	"github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"go.uber.org/zap"
)

func TestApplyNetworkOptionsSetsDCListAndResolver(t *testing.T) {
	t.Parallel()

	cfg := viper.NewConfig()
	cfg.Set("network.dc", 4)
	cfg.Set("network.test_dc", true)
	cfg.Set("network.dial_timeout", 3*time.Second)
	cfg.Set("network.resolver", "plain")
	cfg.Set("network.prefer_ipv6", true)

	options := tgclient.Options{}
	if err := applyNetworkOptions(cfg, &options); err != nil {
		t.Fatalf("applyNetworkOptions() error = %v", err)
	}

	if options.DC != 4 {
		t.Fatalf("expected DC=4, got %d", options.DC)
	}

	if options.DialTimeout != 3*time.Second {
		t.Fatalf("expected dial timeout 3s, got %v", options.DialTimeout)
	}

	if options.Resolver == nil {
		t.Fatal("expected resolver to be configured")
	}

	if !options.DCList.Test {
		t.Fatal("expected test DC list to be enabled")
	}

	if len(options.DCList.Options) == 0 {
		t.Fatal("expected test DC list options to be populated")
	}
}

func TestApplyNetworkOptionsRejectsInvalidResolverConfig(t *testing.T) {
	t.Parallel()

	t.Run("UnsupportedResolver", func(t *testing.T) {
		cfg := viper.NewConfig()
		cfg.Set("network.resolver", "bogus")

		options := tgclient.Options{}
		err := applyNetworkOptions(cfg, &options)
		if err == nil {
			t.Fatal("expected error for unsupported resolver")
		}
	})

	t.Run("MissingSocksAddress", func(t *testing.T) {
		cfg := viper.NewConfig()
		cfg.Set("network.resolver", "socks5")

		options := tgclient.Options{}
		err := applyNetworkOptions(cfg, &options)
		if err == nil {
			t.Fatal("expected error for missing socks5 address")
		}
	})

	t.Run("InvalidMTProxySecret", func(t *testing.T) {
		cfg := viper.NewConfig()
		cfg.Set("network.resolver", "mtproxy")
		cfg.Set("network.mtproxy.address", "127.0.0.1:443")
		cfg.Set("network.mtproxy.secret", "not-hex")

		options := tgclient.Options{}
		err := applyNetworkOptions(cfg, &options)
		if err == nil {
			t.Fatal("expected error for invalid MTProxy secret")
		}
	})
}

func TestApplyReliabilityOptionsSetsBackoffAndHooks(t *testing.T) {
	t.Parallel()

	cfg := viper.NewConfig()
	cfg.Set("connection.migration_timeout", 12*time.Second)
	cfg.Set("connection.log_on_dead", true)
	cfg.Set("connection.reconnect.initial_interval", 250*time.Millisecond)
	cfg.Set("connection.reconnect.max_interval", 5*time.Second)
	cfg.Set("connection.reconnect.max_elapsed_time", time.Minute)
	cfg.Set("connection.reconnect.multiplier", 2.0)
	cfg.Set("connection.reconnect.randomization_factor", 0.25)

	options := tgclient.Options{}
	applyReliabilityOptions(cfg, zap.NewNop(), &options)

	if options.MigrationTimeout != 12*time.Second {
		t.Fatalf("expected migration timeout 12s, got %v", options.MigrationTimeout)
	}

	if options.OnDead == nil {
		t.Fatal("expected OnDead hook to be configured")
	}

	if options.ReconnectionBackoff == nil {
		t.Fatal("expected ReconnectionBackoff to be configured")
	}

	exponential, ok := options.ReconnectionBackoff().(*backoff.ExponentialBackOff)
	if !ok {
		t.Fatalf("expected ExponentialBackOff, got %T", options.ReconnectionBackoff())
	}

	if exponential.InitialInterval != 250*time.Millisecond {
		t.Fatalf("expected initial interval 250ms, got %v", exponential.InitialInterval)
	}
	if exponential.MaxInterval != 5*time.Second {
		t.Fatalf("expected max interval 5s, got %v", exponential.MaxInterval)
	}
	if exponential.MaxElapsedTime != time.Minute {
		t.Fatalf("expected max elapsed time 1m, got %v", exponential.MaxElapsedTime)
	}
	if exponential.Multiplier != 2.0 {
		t.Fatalf("expected multiplier 2.0, got %v", exponential.Multiplier)
	}
	if exponential.RandomizationFactor != 0.25 {
		t.Fatalf("expected randomization factor 0.25, got %v", exponential.RandomizationFactor)
	}
}

func TestNewClientDisablesUpdatesWhenConfigured(t *testing.T) {
	t.Parallel()

	cachePath := t.TempDir() + "/cache.db"

	cfg := viper.NewConfig()
	cfg.Set("app.id", 1)
	cfg.Set("app.hash", "hash")
	cfg.Set("cache.path", cachePath)
	cfg.Set("rate.limit", time.Millisecond)
	cfg.Set("rate.burst", 1)
	cfg.Set("updates.disable", true)

	client, err := NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if !client.disableUpdates {
		t.Fatal("expected disableUpdates to be true")
	}

	if client.updMgr != nil {
		t.Fatalf("expected updMgr to be nil when updates are disabled, got %v", client.updMgr)
	}
}

type testContextDialer struct {
	conn    net.Conn
	err     error
	ctx     context.Context
	network string
	address string
	called  bool
}

func (d *testContextDialer) Dial(network, address string) (net.Conn, error) {
	return nil, errors.New("unexpected Dial call")
}

func (d *testContextDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.called = true
	d.ctx = ctx
	d.network = network
	d.address = address
	return d.conn, d.err
}

type blockingDialer struct {
	started chan struct{}
	release chan struct{}
	conn    net.Conn
	err     error
}

func (d *blockingDialer) Dial(network, address string) (net.Conn, error) {
	close(d.started)
	<-d.release
	return d.conn, d.err
}

func TestProxyDialContextUsesContextDialer(t *testing.T) {
	t.Parallel()

	left, right := net.Pipe()
	defer func() {
		_ = left.Close()
		_ = right.Close()
	}()

	dialer := &testContextDialer{conn: left}
	dial := proxyDialContext(dialer)

	ctx := context.WithValue(context.Background(), struct{}{}, "value")
	conn, err := dial(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial error = %v", err)
	}
	if conn != left {
		t.Fatalf("expected original connection to be returned")
	}
	if !dialer.called {
		t.Fatal("expected DialContext to be called")
	}
	if dialer.ctx != ctx {
		t.Fatal("expected same context to be passed through")
	}
	if dialer.network != "tcp" || dialer.address != "example.com:443" {
		t.Fatalf("unexpected call args: %q %q", dialer.network, dialer.address)
	}
}

func TestProxyDialContextReturnsOnCancelForPlainDialer(t *testing.T) {
	t.Parallel()

	dialer := &blockingDialer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	dial := proxyDialContext(dialer)

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() {
		_, err := dial(ctx, "tcp", "example.com:443")
		errC <- err
	}()

	<-dialer.started
	cancel()

	select {
	case err := <-errC:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dial did not return after cancellation")
	}

	close(dialer.release)
}

func TestProxyDialContextClosesLateConnectionAfterCancel(t *testing.T) {
	t.Parallel()

	left, right := net.Pipe()
	defer func() {
		_ = right.Close()
	}()

	dialer := &blockingDialer{
		started: make(chan struct{}),
		release: make(chan struct{}),
		conn:    left,
	}
	dial := proxyDialContext(dialer)

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() {
		_, err := dial(ctx, "tcp", "example.com:443")
		errC <- err
	}()

	<-dialer.started
	cancel()

	if err := <-errC; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	close(dialer.release)

	readErrC := make(chan error, 1)
	go func() {
		_, err := right.Read(make([]byte, 1))
		readErrC <- err
	}()

	select {
	case err := <-readErrC:
		if err == nil {
			t.Fatal("expected closed connection error, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("late connection was not closed after cancellation")
	}
}
