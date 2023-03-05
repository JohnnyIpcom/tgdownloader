package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/gotd-contrib/storage"
	"go.uber.org/zap"
)

type PeerCacheInfo struct {
	ID         int64
	AccessHash int64
	CreatedAt  time.Time
}

type CacheClient interface {
	UpdateDialogCache(ctx context.Context) error
	GetPeersFromCache(ctx context.Context) (<-chan PeerCacheInfo, error)
}

func (c *client) UpdateDialogCache(ctx context.Context) error {
	if c.peerStore == nil {
		return fmt.Errorf("peer store is not set")
	}

	query := query.GetDialogs(c.client.API())
	return query.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
		return c.storeDialog(ctx, elem)
	})
}

func (c *client) GetPeersFromCache(ctx context.Context) (<-chan PeerCacheInfo, error) {
	if c.peerStore == nil {
		return nil, fmt.Errorf("peer store is not set")
	}

	iter, err := c.peerStore.Iterate(ctx)
	if err != nil {
		return nil, err
	}

	peerCacheInfo := make(chan PeerCacheInfo)
	go func() {
		defer close(peerCacheInfo)

		storage.ForEach(ctx, iter, func(p storage.Peer) error {
			peerCacheInfo <- PeerCacheInfo{
				ID:         p.Key.ID,
				AccessHash: p.Key.AccessHash,
				CreatedAt:  time.Unix(p.CreatedAt, 0),
			}

			return nil
		})
	}()

	return peerCacheInfo, nil
}

func (c *client) storeDialog(ctx context.Context, elem dialogs.Elem) error {
	if c.peerStore == nil {
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

	c.logger.Debug("store peer", zap.Any("peer", p))
	return c.peerStore.Add(ctx, p)
}
