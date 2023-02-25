package telegram

import (
	"bufio"
	"context"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/key"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

type Client interface {
	ChatClient
	FileClient
	UserClient

	Run(ctx context.Context, f func(context.Context, Client) error, opts ...RunOption) error
}

type client struct {
	config     config.Config
	logger     *zap.Logger
	client     *tgclient.Client
	keys       []tgclient.PublicKey
	peerMgr    *peers.Manager
	updMgr     *updates.Manager
	session    session.Loader
	downloader *downloader.Downloader
	dispatcher tg.UpdateDispatcher
}

var _ Client = (*client)(nil)

func NewPublicKey(key *rsa.PublicKey) tgclient.PublicKey {
	return tgclient.PublicKey{
		RSA: key,
	}
}

func NewClient(cfg config.Config, log *zap.Logger) (Client, error) {
	storage := &session.FileStorage{
		Path: cfg.GetString("telegram.session.path"),
	}

	var keys []tgclient.PublicKey

	publicKeys := cfg.GetStringSlice("telegram.mtproto.public_keys")
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

	d := tg.NewUpdateDispatcher()
	gaps := updates.New(updates.Config{
		Handler: d,
		Logger:  log.Named("gaps"),
	})

	c := tgclient.NewClient(cfg.GetInt("telegram.app.id"), cfg.GetString("telegram.app.hash"), tgclient.Options{
		Logger:         log.Named("client"),
		SessionStorage: storage,
		PublicKeys:     keys,
		UpdateHandler:  gaps,
		Middlewares: []tgclient.Middleware{
			ratelimit.New(
				rate.Every(cfg.GetDuration("telegram.rate.limit")),
				cfg.GetInt("telegram.rate.burst"),
			),
			floodwait.NewSimpleWaiter(),
			hook.UpdateHook(gaps.Handle),
		},
	})

	peerMgr := peers.Options{
		Logger: log.Named("peers"),
	}.Build(c.API())

	return &client{
		config:     cfg,
		logger:     log,
		client:     c,
		keys:       keys,
		peerMgr:    peerMgr,
		updMgr:     gaps,
		session:    session.Loader{Storage: storage},
		downloader: downloader.NewDownloader(),
		dispatcher: d,
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
