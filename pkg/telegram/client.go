package telegram

import (
	"bufio"
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"github.com/johnnyipcom/tgdownloader/pkg/key"
	bboltdb "go.etcd.io/bbolt"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// StopFunc is a function that stops service.
type StopFunc func() error

// LogoutFunc is a function that logs out from Telegram.
type LogoutFunc func() error

// Client is a Telegram client.
type Client struct {
	config     config.Config
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

func (c *Client) Auth(ctx context.Context) (LogoutFunc, error) {
	fmt.Println("Authenticating...")
	flow := auth.NewFlow(
		auth.Constant(
			c.config.GetString("phone"),
			c.config.GetString("password"),
			&codeAuthenticator{}),
		auth.SendCodeOptions{},
	)

	if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
		return func() error { return nil }, fmt.Errorf("auth: %w", err)
	}

	user, err := c.client.Self(ctx)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("fetch self: %w", err)
	}

	username, _ := user.GetUsername()
	fmt.Printf("Authenticated as '%s'\n", username)

	fmt.Println("Notifying updates manager...")
	if err := c.updMgr.Auth(ctx, c.client.API(), user.GetID(), false, true); err != nil {
		return func() error { return nil }, fmt.Errorf("auth updates: %w", err)
	}

	fmt.Println("Done")
	return func() error {
		fmt.Println("\nLogging out...")
		if err := c.updMgr.Logout(); err != nil {
			return err
		}

		fmt.Println("Done")
		return nil
	}, nil
}

// Connect connects to Telegram.
func (c *Client) Connect(ctx context.Context) (StopFunc, error) {
	ctx, cancel := context.WithCancel(ctx)

	errC := make(chan error, 1)
	initDone := make(chan struct{})
	go func() {
		defer close(errC)
		errC <- c.client.Run(ctx, func(ctx context.Context) error {
			logout, err := c.Auth(ctx)
			if err != nil {
				return err
			}

			defer func() {
				logger := ctxlogger.FromContext(ctx)
				if err := logout(); err != nil {
					logger.Error("logout", zap.Error(err))
				}
			}()
			close(initDone)

			<-ctx.Done()
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}

			return ctx.Err()
		})
	}()

	select {
	case <-ctx.Done():
		cancel()
		return func() error { return nil }, ctx.Err()

	case err := <-errC:
		cancel()
		return func() error { return nil }, err

	case <-initDone:
	}

	stopFn := func() error {
		cancel()
		return <-errC
	}

	return stopFn, nil
}

// Run runs the function f with the client.
func (c *Client) Run(ctx context.Context, f func(context.Context, *Client) error) error {
	return c.client.Run(ctx, func(ctx context.Context) error {
		logout, err := c.Auth(ctx)
		if err != nil {
			return err
		}

		defer func() {
			logger := ctxlogger.FromContext(ctx)
			if err := logout(); err != nil {
				logger.Error("logout", zap.Error(err))
			}
		}()

		return f(ctx, c)
	})
}

func (c *Client) API() *tg.Client {
	return c.client.API()
}

func (c *Client) getInputPeer(ctx context.Context, ID constant.TDLibPeerID) (tg.InputPeerClass, error) {
	if c.storage != nil {
		if peer, err := c.storage.Resolve(ctx, strconv.FormatInt(ID.ToPlain(), 10)); err == nil {
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

	var key string

	var p storage.Peer
	switch dlg := elem.Dialog.GetPeer().(type) {
	case *tg.PeerUser:
		key = strconv.FormatInt(dlg.UserID, 10)
		user, ok := elem.Entities.User(dlg.UserID)
		if !ok || !p.FromUser(user) {
			return fmt.Errorf("user not found: %d", dlg.UserID)
		}

	case *tg.PeerChat:
		key = strconv.FormatInt(dlg.ChatID, 10)
		chat, ok := elem.Entities.Chat(dlg.ChatID)
		if !ok || !p.FromChat(chat) {
			return fmt.Errorf("chat not found: %d", dlg.ChatID)
		}

	case *tg.PeerChannel:
		key = strconv.FormatInt(dlg.ChannelID, 10)
		channel, ok := elem.Entities.Channel(dlg.ChannelID)
		if !ok || !p.FromChat(channel) {
			return fmt.Errorf("channel not found: %d", dlg.ChannelID)
		}
	}

	return c.storage.Assign(ctx, key, p)
}
