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
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

type Client struct {
	config config.Config
	logger *zap.Logger
	client *tgclient.Client
}

func NewClient(cfg config.Config, log *zap.Logger) (*Client, error) {
	storage := &session.FileStorage{
		Path: cfg.GetString("telegram.session.path"),
	}

	client := tgclient.NewClient(cfg.GetInt("telegram.app.id"), cfg.GetString("telegram.app.hash"), tgclient.Options{
		Logger:         log,
		SessionStorage: storage,
		Middlewares: []tgclient.Middleware{
			ratelimit.New(
				rate.Every(cfg.GetDuration("telegram.rate.limit")),
				cfg.GetInt("telegram.rate.burst"),
			),
		},
	})

	return &Client{
		config: cfg,
		logger: log,
		client: client,
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

func (c *Client) Run(ctx context.Context, f func(context.Context, *tg.Client) error) {
	err := c.client.Run(ctx, func(ctx context.Context) error {
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
		return f(ctx, c.client.API())
	})

	if err != nil {
		c.logger.Error("client run", zap.Error(err))
	}
}
