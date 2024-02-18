package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
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
	logger     *zap.Logger
	peerMgr    *peers.Manager
	updMgr     *updates.Manager
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
	logger *zap.Logger
}

// NewClient creates new Telegram client.
func NewClient(cfg config.Config, log *zap.Logger) (*Client, error) {
	dispatcher := tg.NewUpdateDispatcher()

	db, err := bboltdb.Open(cfg.GetString("cache.path"), 0600, nil)
	if err != nil {
		return nil, err
	}

	peerStorage := bbolt.NewPeerStorage(db, []byte("peers"))

	var handler tgclient.UpdateHandler = dispatcher
	if peerStorage != nil {
		handler = storage.UpdateHook(dispatcher, peerStorage)
	}

	gaps := updates.New(updates.Config{
		Handler: handler,
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

	if cfg.IsSet("session.path") {
		options.SessionStorage = &session.FileStorage{
			Path: cfg.GetString("session.path"),
		}
	}

	keys, err := getPublicKeys(cfg)
	if err != nil {
		return nil, err
	}

	if len(keys) > 0 {
		options.PublicKeys = keys
	}

	c := tgclient.NewClient(cfg.GetInt("app.id"), cfg.GetString("app.hash"), options)

	peerMgr := peers.Options{
		Logger: log.Named("peers"),
	}.Build(c.API())

	cli := &Client{
		config:     cfg,
		client:     c,
		logger:     log,
		peerMgr:    peerMgr,
		updMgr:     gaps,
		dispatcher: dispatcher,
		storage:    peerStorage,
	}

	// Set up services
	cli.common.client = cli
	cli.common.logger = log.Named("service")
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

	authorized := make(chan struct{})
	authOptions := updates.AuthOptions{
		IsBot:  true,
		Forget: true,
		OnStart: func(ctx context.Context) {
			fmt.Println("Starting updates manager...")
			close(authorized)
		},
	}

	go func() {
		updErr := c.updMgr.Run(ctx, c.client.API(), user.GetID(), authOptions)
		if updErr != nil && !errors.Is(updErr, context.Canceled) {
			fmt.Printf("auth updates error: %s\n", err)
		}
	}()

	<-authorized

	fmt.Println("Done")
	return func() error {
		fmt.Println("\nLogging out...")
		c.updMgr.Reset()
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
				if err := logout(); err != nil {
					c.logger.Error("logout", zap.Error(err))
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
			if err := logout(); err != nil {
				c.logger.Error("logout", zap.Error(err))
			}
		}()

		return f(ctx, c)
	})
}

func (c *Client) API() *tg.Client {
	return c.client.API()
}

func (c *Client) ExtractPeer(ctx context.Context, ent peer.Entities, peerID tg.PeerClass) (peers.Peer, error) {
	peer, err := ent.ExtractPeer(peerID)
	if err != nil {
		return nil, fmt.Errorf("extract peer: %w", err)
	}

	return c.peerMgr.FromInputPeer(ctx, peer)
}

func (c *Client) GetInputPeer(ctx context.Context, ID constant.TDLibPeerID) (tg.InputPeerClass, error) {
	if peer, err := c.storage.Resolve(ctx, strconv.FormatInt(ID.ToPlain(), 10)); err == nil {
		return peer.AsInputPeer(), nil
	} else {
		if !errors.Is(err, storage.ErrPeerNotFound) {
			return nil, err
		}
	}

	peer, err := c.peerMgr.ResolveTDLibID(ctx, ID)
	if err != nil {
		return nil, err
	}

	return peer.InputPeer(), nil
}

func (c *Client) CacheDialog(ctx context.Context, elem dialogs.Elem) error {
	var p storage.Peer

	switch dlg := elem.Dialog.GetPeer().(type) {
	case *tg.PeerUser:
		user, ok := elem.Entities.User(dlg.UserID)
		if !ok || !p.FromUser(user) {
			return nil
		}

	case *tg.PeerChat:
		chat, ok := elem.Entities.Chat(dlg.ChatID)
		if !ok || !p.FromChat(chat) {
			return nil
		}

	case *tg.PeerChannel:
		channel, ok := elem.Entities.Channel(dlg.ChannelID)
		if !ok || !p.FromChat(channel) {
			return nil
		}
	}

	return c.storage.Add(ctx, p)
}

func (c *Client) CacheInputPeer(ctx context.Context, inputPeer tg.InputPeerClass) error {
	var p storage.Peer

	if err := p.FromInputPeer(inputPeer); err != nil {
		return err
	}

	return c.storage.Add(ctx, p)
}
