package telegram

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

type Client interface {
	ChatClient
	FileClient

	Run(ctx context.Context, f func(context.Context, Client) error) error
}

type client struct {
	config     config.Config
	logger     *zap.Logger
	client     *tgclient.Client
	downloader *downloader.Downloader
}

var _ Client = (*client)(nil)

func NewClient(cfg config.Config, log *zap.Logger) (Client, error) {
	storage := &session.FileStorage{
		Path: cfg.GetString("telegram.session.path"),
	}

	c := tgclient.NewClient(cfg.GetInt("telegram.app.id"), cfg.GetString("telegram.app.hash"), tgclient.Options{
		Logger:         log,
		SessionStorage: storage,
		Middlewares: []tgclient.Middleware{
			ratelimit.New(
				rate.Every(cfg.GetDuration("telegram.rate.limit")),
				cfg.GetInt("telegram.rate.burst"),
			),
		},
	})

	return &client{
		config:     cfg,
		logger:     log,
		client:     c,
		downloader: downloader.NewDownloader(),
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

func (c *client) Run(ctx context.Context, f func(context.Context, Client) error) error {
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
		return f(ctx, c)
	})
}
