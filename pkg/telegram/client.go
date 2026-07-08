package telegram

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/session"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	bboltdb "go.etcd.io/bbolt"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"
)

type linkedChatPeer interface {
	peers.Peer
	IsBroadcast() bool
	FullRaw(ctx context.Context) (*tg.ChannelFull, error)
}

// StopFunc is a function that stops service.
type StopFunc func() error

// LogoutFunc is a function that logs out from Telegram.
type LogoutFunc func() error

// Client is a Telegram client.
type Client struct {
	config         config.Config
	client         *tgclient.Client
	disableUpdates bool
	logger         *zap.Logger
	db             *bboltdb.DB
	peerMgr        *peers.Manager
	updMgr         *updates.Manager
	dispatcher     tg.UpdateDispatcher
	storage        storage.PeerStorage
	progress       Progress

	common service // Reuse a single struct instead of allocating one for each service on the heap

	// Add other services here
	UserService   UserService
	PeerService   PeerService
	FileService   FileService
	LinkService   LinkService
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
	disableUpdates := cfg.GetBool("updates.disable")

	db, err := bboltdb.Open(cfg.GetString("cache.path"), 0600, nil)
	if err != nil {
		return nil, err
	}

	peerStorage := bbolt.NewPeerStorage(db, []byte("peers"))

	middlewares := []tgclient.Middleware{
		ratelimit.New(
			rate.Every(cfg.GetDuration("rate.limit")),
			cfg.GetInt("rate.burst"),
		),
		newFloodWaiter(cfg, log),
	}

	options := tgclient.Options{
		Logger:      log.Named("client"),
		AllowCDN:    true,
		NoUpdates:   disableUpdates,
		Middlewares: middlewares,
	}

	var gaps *updates.Manager
	if !disableUpdates {
		var handler tgclient.UpdateHandler = dispatcher
		if peerStorage != nil {
			handler = storage.UpdateHook(dispatcher, peerStorage)
		}

		gapsLog := log.Named("gaps")
		gaps = updates.New(updates.Config{
			Handler:      handler,
			Storage:      bbolt.NewStateStorage(db),
			AccessHasher: newBoltChannelAccessHasher(db),
			OnChannelTooLong: func(channelID int64) {
				gapsLog.Warn("channel update gap too long",
					zap.Int64("channel_id", channelID),
				)
			},
			Logger: gapsLog,
		})

		options.UpdateHandler = gaps
		options.Middlewares = append(options.Middlewares, hook.UpdateHook(gaps.Handle))
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

	clock, err := getClock(cfg)
	if err != nil {
		return nil, err
	}

	if clock != nil {
		options.Clock = clock
	}

	if cfg.IsSet("download.allow_cdn") {
		options.AllowCDN = cfg.GetBool("download.allow_cdn")
	}

	if err := applyNetworkOptions(cfg, &options); err != nil {
		return nil, err
	}

	applyReliabilityOptions(cfg, log, &options)

	c := tgclient.NewClient(cfg.GetInt("app.id"), cfg.GetString("app.hash"), options)

	peerMgr := peers.Options{
		Logger: log.Named("peers"),
	}.Build(c.API())

	cli := &Client{
		config:         cfg,
		client:         c,
		disableUpdates: disableUpdates,
		logger:         log,
		db:             db,
		peerMgr:        peerMgr,
		updMgr:         gaps,
		dispatcher:     dispatcher,
		storage:        peerStorage,
		progress:       &progress{},
	}

	// Set up services
	cli.common.client = cli
	cli.common.logger = log.Named("service")
	cli.UserService = (*userService)(&cli.common)
	cli.PeerService = (*peerService)(&cli.common)
	cli.FileService = (*fileService)(&cli.common)
	cli.LinkService = (*linkService)(&cli.common)
	cli.DialogService = (*dialogService)(&cli.common)
	cli.CacheService = (*cacheService)(&cli.common)
	return cli, nil
}

func applyNetworkOptions(cfg config.Config, options *tgclient.Options) error {
	if cfg.IsSet("network.dc") {
		options.DC = cfg.GetInt("network.dc")
	}

	if cfg.GetBool("network.test_dc") {
		options.DCList = dcs.Test()
	}

	if cfg.IsSet("network.dial_timeout") {
		options.DialTimeout = cfg.GetDuration("network.dial_timeout")
	}

	resolver, err := buildResolver(cfg, options.DialTimeout)
	if err != nil {
		return err
	}
	if resolver != nil {
		options.Resolver = resolver
	}

	return nil
}

func applyReliabilityOptions(cfg config.Config, log *zap.Logger, options *tgclient.Options) {
	if cfg.IsSet("connection.migration_timeout") {
		options.MigrationTimeout = cfg.GetDuration("connection.migration_timeout")
	}

	if cfg.GetBool("connection.log_on_dead") {
		onDeadLogger := log.Named("telegram-conn")
		options.OnDead = func(err error) {
			onDeadLogger.Warn("connection dead", zap.Error(err))
		}
	}

	if hasReconnectBackoffConfig(cfg) {
		options.ReconnectionBackoff = func() backoff.BackOff {
			exponential := backoff.NewExponentialBackOff()
			if cfg.IsSet("connection.reconnect.initial_interval") {
				exponential.InitialInterval = cfg.GetDuration("connection.reconnect.initial_interval")
			}
			if cfg.IsSet("connection.reconnect.max_interval") {
				exponential.MaxInterval = cfg.GetDuration("connection.reconnect.max_interval")
			}
			if cfg.IsSet("connection.reconnect.max_elapsed_time") {
				exponential.MaxElapsedTime = cfg.GetDuration("connection.reconnect.max_elapsed_time")
			}
			if cfg.IsSet("connection.reconnect.multiplier") {
				exponential.Multiplier = cfg.GetFloat64("connection.reconnect.multiplier")
			}
			if cfg.IsSet("connection.reconnect.randomization_factor") {
				exponential.RandomizationFactor = cfg.GetFloat64("connection.reconnect.randomization_factor")
			}
			exponential.Reset()
			return exponential
		}
	}
}

func newFloodWaiter(cfg config.Config, log *zap.Logger) tgclient.Middleware {
	waiter := floodwait.NewWaiter()
	if !cfg.IsSet("flood_wait.log") || cfg.GetBool("flood_wait.log") {
		waiter = waiter.WithCallback(func(ctx context.Context, wait floodwait.FloodWait) {
			log.Named("floodwait").Warn("telegram flood wait",
				zap.Duration("wait", wait.Duration),
			)
		})
	}

	return waiter
}

func hasReconnectBackoffConfig(cfg config.Config) bool {
	return cfg.IsSet("connection.reconnect.initial_interval") ||
		cfg.IsSet("connection.reconnect.max_interval") ||
		cfg.IsSet("connection.reconnect.max_elapsed_time") ||
		cfg.IsSet("connection.reconnect.multiplier") ||
		cfg.IsSet("connection.reconnect.randomization_factor")
}

func buildResolver(cfg config.Config, dialTimeout time.Duration) (dcs.Resolver, error) {
	resolverName := strings.ToLower(strings.TrimSpace(cfg.GetString("network.resolver")))
	needsPlainResolver := resolverName == "plain" || resolverName == "direct" || resolverName == "env" || resolverName == "socks5" ||
		cfg.IsSet("network.prefer_ipv6") || cfg.IsSet("network.no_obfuscated")

	if resolverName == "" && !needsPlainResolver {
		return nil, nil
	}

	baseDialer := &net.Dialer{Timeout: dialTimeout}

	switch resolverName {
	case "", "default", "plain", "direct":
		return dcs.Plain(plainResolverOptions(cfg, baseDialer.DialContext)), nil

	case "env":
		envDialer := proxy.FromEnvironmentUsing(baseDialer)
		return dcs.Plain(plainResolverOptions(cfg, proxyDialContext(envDialer))), nil

	case "socks5":
		proxyAddress := strings.TrimSpace(cfg.GetString("network.proxy.address"))
		if proxyAddress == "" {
			return nil, errors.New("telegram.network.proxy.address is required for socks5 resolver")
		}

		var auth *proxy.Auth
		proxyUser := cfg.GetString("network.proxy.user")
		if proxyUser != "" || cfg.IsSet("network.proxy.password") {
			auth = &proxy.Auth{
				User:     proxyUser,
				Password: cfg.GetString("network.proxy.password"),
			}
		}

		socksDialer, err := proxy.SOCKS5("tcp", proxyAddress, auth, baseDialer)
		if err != nil {
			return nil, fmt.Errorf("create socks5 dialer: %w", err)
		}

		return dcs.Plain(plainResolverOptions(cfg, proxyDialContext(socksDialer))), nil

	case "mtproxy":
		proxyAddress := strings.TrimSpace(cfg.GetString("network.mtproxy.address"))
		if proxyAddress == "" {
			return nil, errors.New("telegram.network.mtproxy.address is required for mtproxy resolver")
		}

		secretHex := strings.TrimSpace(cfg.GetString("network.mtproxy.secret"))
		if secretHex == "" {
			return nil, errors.New("telegram.network.mtproxy.secret is required for mtproxy resolver")
		}

		secret, err := hex.DecodeString(secretHex)
		if err != nil {
			return nil, fmt.Errorf("decode mtproxy secret: %w", err)
		}

		return dcs.MTProxy(proxyAddress, secret, dcs.MTProxyOptions{
			Dial:    baseDialer.DialContext,
			Network: cfg.GetString("network.mtproxy.network"),
		})
	default:
		return nil, fmt.Errorf("unsupported telegram.network.resolver %q", resolverName)
	}
}

func plainResolverOptions(cfg config.Config, dial dcs.DialFunc) dcs.PlainOptions {
	return dcs.PlainOptions{
		Dial:         dial,
		PreferIPv6:   cfg.GetBool("network.prefer_ipv6"),
		NoObfuscated: cfg.GetBool("network.no_obfuscated"),
	}
}

func proxyDialContext(dialer proxy.Dialer) dcs.DialFunc {
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		return contextDialer.DialContext
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		type dialResult struct {
			conn net.Conn
			err  error
		}

		result := make(chan dialResult)
		go func() {
			conn, err := dialer.Dial(network, address)
			res := dialResult{conn: conn, err: err}
			select {
			case result <- res:
			case <-ctx.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-result:
			if ctx.Err() != nil {
				if res.conn != nil {
					_ = res.conn.Close()
				}
				return nil, ctx.Err()
			}
			return res.conn, res.err
		}
	}
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

func (c *Client) SetProgress(r Progress) {
	c.progress = r
}

func (c *Client) Auth(ctx context.Context) (LogoutFunc, error) {
	authTracker := c.progress.Tracker("Authentication")
	flow := auth.NewFlow(
		auth.Constant(
			c.config.GetString("phone"),
			c.config.GetString("password"),
			&codeAuthenticator{}),
		auth.SendCodeOptions{},
	)

	if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
		authTracker.Fail()
		return func() error { return nil }, fmt.Errorf("auth: %w", err)
	}

	authTracker.Done()
	c.progress.Wait(ctx)

	if c.disableUpdates || c.updMgr == nil {
		return func() error { return nil }, nil
	}

	user, err := c.client.Self(ctx)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("fetch self: %w", err)
	}

	updateTracker := c.progress.Tracker("Update tracker")

	updateStarted := make(chan struct{})
	authOptions := updates.AuthOptions{
		IsBot: user.GetBot(),
		OnStart: func(ctx context.Context) {
			updateTracker.Done()
			close(updateStarted)
		},
	}

	go func() {
		updErr := c.updMgr.Run(ctx, c.client.API(), user.GetID(), authOptions)
		if updErr != nil && !errors.Is(updErr, context.Canceled) {
			updateTracker.Fail()
			fmt.Printf("auth updates error: %s\n", updErr)
		}
	}()

	<-updateStarted
	c.progress.Wait(ctx)
	return func() error {
		logoutTracker := c.progress.Tracker("Logout")
		c.updMgr.Reset()
		logoutTracker.Done()
		c.progress.Wait(ctx)
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

func (c *Client) Close() error {
	if c.db == nil {
		return nil
	}

	err := c.db.Close()
	c.db = nil
	return err
}

// ParseMessageLink return peer, msgId, error
func (c *Client) ParseMessageLink(ctx context.Context, s string) (peers.Peer, int, error) {
	parse := func(from, msg string) (peers.Peer, int, error) {
		ch, err := c.ResolvePeer(ctx, from)
		if err != nil {
			return nil, 0, err
		}

		m, err := strconv.Atoi(msg)
		if err != nil {
			return nil, 0, err
		}

		return ch, m, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return nil, 0, err
	}

	paths := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	// https://t.me/opencfdchannel/4434?comment=360409
	if comment := u.Query().Get("comment"); comment != "" {
		peer, err := c.ResolvePeer(ctx, paths[0])
		if err != nil {
			return nil, 0, err
		}

		ch, ok := peer.(linkedChatPeer)
		if !ok || !ch.IsBroadcast() {
			return nil, 0, fmt.Errorf("comment links require a broadcast channel")
		}

		raw, err := ch.FullRaw(ctx)
		if err != nil {
			return nil, 0, err
		}

		linked, ok := raw.GetLinkedChatID()
		if !ok {
			return nil, 0, errors.New("no linked chat")
		}

		return parse(strconv.FormatInt(linked, 10), comment)
	}

	switch len(paths) {
	case 2:
		// https://t.me/telegram/193
		// https://t.me/myhostloc/1485524?thread=1485523
		return parse(paths[0], paths[1])
	case 3:
		// https://t.me/c/1697797156/151
		// https://t.me/iFreeKnow/45662/55005
		if paths[0] == "c" {
			return parse(paths[1], paths[2])
		}

		// "45662" means topic id, we don't need it
		return parse(paths[0], paths[2])
	case 4:
		// https://t.me/c/1492447836/251015/251021
		if paths[0] != "c" {
			return nil, 0, fmt.Errorf("invalid message link")
		}

		// "251015" means topic id, we don't need it
		return parse(paths[1], paths[3])
	default:
		return nil, 0, fmt.Errorf("invalid message link: %s", s)
	}
}

func (c *Client) ExtractPeer(ctx context.Context, ent peer.Entities, peerID tg.PeerClass) (peers.Peer, error) {
	peer, err := ent.ExtractPeer(peerID)
	if err != nil {
		return nil, fmt.Errorf("extract peer: %w", err)
	}

	return c.peerMgr.FromInputPeer(ctx, peer)
}

func (c *Client) ResolvePeer(ctx context.Context, from string) (peers.Peer, error) {
	id, err := strconv.ParseInt(from, 10, 64)
	if err != nil {
		p, err := c.PeerService.Resolve(ctx, from)
		if err != nil {
			return nil, err
		}

		return p, nil
	}

	return c.PeerService.ResolveID(ctx, id)
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
