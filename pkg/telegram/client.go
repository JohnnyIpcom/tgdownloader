package telegram

import (
	"bufio"
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
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

// RunnerFunc is a function that runs on client.
type RunnerFunc func(context.Context, *Client) error

// Client is a Telegram client.
type Client struct {
	config     config.Config
	logger     *zap.Logger
	client     *tgclient.Client
	peerMgr    *peers.Manager
	updMgr     *updates.Manager
	downloader *downloader.Downloader
	dispatcher tg.UpdateDispatcher
	storage    storage.PeerStorage

	common service // Reuse a single struct instead of allocating one for each service on the heap

	// Add other services here
	UserService   UserService
	PeerService   PeerService
	FileService   FileService
	DialogService DialogService
	CacheService  CacheService
}

type service struct {
	client *Client
}

func newPublicKey(key *rsa.PublicKey) tgclient.PublicKey {
	return tgclient.PublicKey{
		RSA: key,
	}
}

// NewClient creates new Telegram client.
func NewClient(cfg config.Config, log *zap.Logger) (*Client, error) {
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

	var storage storage.PeerStorage
	if cfg.IsSet("cache.path") {
		db, err := bboltdb.Open(cfg.GetString("cache.path"), 0600, nil)
		if err != nil {
			return nil, err
		}

		storage = bbolt.NewPeerStorage(db, []byte("peers"))
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

			keys = append(keys, newPublicKey(k))
		}

		options.PublicKeys = keys
	}

	c := tgclient.NewClient(cfg.GetInt("app.id"), cfg.GetString("app.hash"), options)

	peerMgr := peers.Options{
		Logger: log.Named("peers"),
	}.Build(c.API())

	cli := &Client{
		config:     cfg,
		logger:     log,
		client:     c,
		peerMgr:    peerMgr,
		updMgr:     gaps,
		downloader: downloader.NewDownloader(),
		dispatcher: dispatcher,
		storage:    storage,
	}

	// Set up services
	cli.common.client = cli
	cli.UserService = (*userService)(&cli.common)
	cli.PeerService = (*peerService)(&cli.common)
	cli.FileService = (*fileService)(&cli.common)
	cli.DialogService = (*dialogService)(&cli.common)
	cli.CacheService = (*cacheService)(&cli.common)
	return cli, nil
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

// Run starts client session.
func (c *Client) Run(ctx context.Context, f RunnerFunc) error {
	return c.client.Run(ctx, func(ctx context.Context) error {
		fmt.Println("Authenticating...")
		flow := auth.NewFlow(
			auth.Constant(
				c.config.GetString("phone"),
				c.config.GetString("password"),
				&codeAuthenticator{}),
			auth.SendCodeOptions{},
		)

		if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("auth: %w", err)
		}

		fmt.Println("Authenticated")
		return f(ctx, c)
	})
}

func (c *Client) WithUpdates(ctx context.Context, f RunnerFunc) RunnerFunc {
	return func(ctx context.Context, client *Client) error {
		user, err := c.client.Self(ctx)
		if err != nil {
			return fmt.Errorf("fetch self: %w", err)
		}

		fmt.Println("Authenticating with updates...")
		if err := c.updMgr.Auth(ctx, c.client.API(), user.GetID(), false, true); err != nil {
			return fmt.Errorf("auth updates: %w", err)
		}

		defer func() {
			if err := c.updMgr.Logout(); err != nil {
				c.logger.Error("logout", zap.Error(err))
			}

			c.logger.Info("logout success")
		}()

		return f(ctx, c)
	}
}

func (c *Client) API() *tg.Client {
	return c.client.API()
}

func (c *Client) getInputPeer(ctx context.Context, ID constant.TDLibPeerID) (tg.InputPeerClass, error) {
	if c.storage != nil {
		var peerClass tg.PeerClass
		switch {
		case ID.IsUser():
			peerClass = &tg.PeerUser{UserID: ID.ToPlain()}

		case ID.IsChat():
			peerClass = &tg.PeerChat{ChatID: ID.ToPlain()}

		case ID.IsChannel():
			peerClass = &tg.PeerChannel{ChannelID: ID.ToPlain()}

		default:
			return nil, errors.New("unknown peer type")
		}

		if peer, err := storage.FindPeer(ctx, c.storage, peerClass); err == nil {
			return peer.AsInputPeer(), nil
		} else {
			if !errors.Is(err, storage.ErrPeerNotFound) {
				return nil, err
			}
		}
	}

	peer, err := c.peerMgr.ResolveTDLibID(ctx, ID)
	if err != nil {
		return nil, err
	}

	return peer.InputPeer(), nil
}

func (c *Client) storeDialog(ctx context.Context, elem dialogs.Elem) error {
	if c.storage == nil {
		return nil
	}

	var p storage.Peer
	switch dlg := elem.Dialog.GetPeer().(type) {
	case *tg.PeerUser:
		user, ok := elem.Entities.User(dlg.UserID)
		if !ok || !p.FromUser(user) {
			return fmt.Errorf("user not found: %d", dlg.UserID)
		}

	case *tg.PeerChat:
		chat, ok := elem.Entities.Chat(dlg.ChatID)
		if !ok || !p.FromChat(chat) {
			return fmt.Errorf("chat not found: %d", dlg.ChatID)
		}

	case *tg.PeerChannel:
		channel, ok := elem.Entities.Channel(dlg.ChannelID)
		if !ok || !p.FromChat(channel) {
			return fmt.Errorf("channel not found: %d", dlg.ChannelID)
		}
	}

	return c.storage.Add(ctx, p)
}
