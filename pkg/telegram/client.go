package telegram

import (
	"bufio"
	"context"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/gotd-contrib/bbolt"
	"github.com/johnnyipcom/gotd-contrib/middleware/floodwait"
	"github.com/johnnyipcom/gotd-contrib/middleware/ratelimit"
	"github.com/johnnyipcom/gotd-contrib/storage"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/key"
	bboltdb "go.etcd.io/bbolt"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

type Client interface {
	PeerClient
	FileClient
	UserClient
	DialogClient
	CacheClient

	Run(ctx context.Context, f func(context.Context, Client) error, opts ...RunOption) error
}

type client struct {
	config     config.Config
	logger     *zap.Logger
	client     *tgclient.Client
	peerMgr    *peers.Manager
	updMgr     *updates.Manager
	downloader *downloader.Downloader
	dispatcher tg.UpdateDispatcher
	peerStore  storage.PeerStorage
}

var _ Client = (*client)(nil)

func NewPublicKey(key *rsa.PublicKey) tgclient.PublicKey {
	return tgclient.PublicKey{
		RSA: key,
	}
}

func NewClient(cfg config.Config, log *zap.Logger) (Client, error) {
	dispatcher := tg.NewUpdateDispatcher()
	gaps := updates.New(updates.Config{
		Handler: dispatcher,
		Logger:  log.Named("gaps"),
	})

	options := tgclient.Options{
		Logger:        log.Named("client"),
		UpdateHandler: gaps,
		Middlewares: []tgclient.Middleware{
			ratelimit.New(
				rate.Every(cfg.GetDuration("rate.limit")),
				cfg.GetInt("rate.burst"),
			),
			floodwait.NewSimpleWaiter(),
			hook.UpdateHook(gaps.Handle),
		},
	}

	var peerStorage storage.PeerStorage
	if cfg.IsSet("storage.path") {
		db, err := bboltdb.Open(cfg.GetString("storage.path"), 0600, nil)
		if err != nil {
			return nil, err
		}

		peerStorage = bbolt.NewPeerStorage(db, []byte("peers"))
	}

	if cfg.IsSet("session.path") {
		options.SessionStorage = &session.FileStorage{
			Path: cfg.GetString("session.path"),
		}
	}

	if cfg.IsSet("mtproto.public_keys") {
		var keys []tgclient.PublicKey

		publicKeys := cfg.GetStringSlice("mtproto.public_keys")
		for _, publicKey := range publicKeys {
			publicKeyData, err := os.ReadFile(publicKey)
			if err != nil {
				return nil, err
			}

			k, err := key.ParsePublicKey(publicKeyData)
			if err != nil {
				return nil, err
			}

			keys = append(keys, NewPublicKey(k))
		}

		options.PublicKeys = keys
	}

	c := tgclient.NewClient(cfg.GetInt("app.id"), cfg.GetString("app.hash"), options)

	peerMgr := peers.Options{
		Logger: log.Named("peers"),
	}.Build(c.API())

	return &client{
		config:     cfg,
		logger:     log,
		client:     c,
		peerMgr:    peerMgr,
		updMgr:     gaps,
		downloader: downloader.NewDownloader(),
		dispatcher: dispatcher,
		peerStore:  peerStorage,
	}, nil
}

type codeAuthenticator struct{}

func (c *codeAuthenticator) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	fmt.Print("Enter code: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(code), nil
}

type runOptions struct {
	infinite bool
}

type RunOption interface {
	apply(*runOptions) error
}

type runInfiniteOption struct{}

func (runInfiniteOption) apply(o *runOptions) error {
	o.infinite = true
	return nil
}

func RunInfinite() RunOption {
	return runInfiniteOption{}
}

// Run starts client session.
func (c *client) Run(ctx context.Context, f func(context.Context, Client) error, opts ...RunOption) error {
	options := runOptions{}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return err
		}
	}

	return c.client.Run(ctx, func(ctx context.Context) error {
		c.logger.Info("auth start")
		flow := auth.NewFlow(
			auth.Constant(
				c.config.GetString("telegram.phone"),
				c.config.GetString("telegram.password"),
				&codeAuthenticator{}),
			auth.SendCodeOptions{},
		)

		if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("auth: %w", err)
		}

		c.logger.Info("auth success")

		user, err := c.client.Self(ctx)
		if err != nil {
			return fmt.Errorf("fetch self: %w", err)
		}

		c.logger.Info("self", zap.Stringer("user", user))
		if !options.infinite {
			return f(ctx, c)
		}

		c.logger.Info("updates start")
		if err := c.updMgr.Auth(ctx, c.client.API(), user.GetID(), false, true); err != nil {
			return fmt.Errorf("auth updates: %w", err)
		}

		defer func() {
			if err := c.updMgr.Logout(); err != nil {
				c.logger.Error("logout", zap.Error(err))
			}

			c.logger.Info("logout success")
		}()

		if err := f(ctx, c); err != nil {
			return fmt.Errorf("run: %w", err)
		}

		<-ctx.Done()
		return ctx.Err()
	})
}
