package telegram

import (
	"context"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/tg"
)

type peerEntityApplier interface {
	Apply(context.Context, []tg.UserClass, []tg.ChatClass) error
}

// applyStoredPeers converts durable entities into the classes expected by
// gotd, restoring access hashes from the canonical storage key.
func (c *Client) applyStoredPeers(ctx context.Context, stored []storage.Peer) error {
	if c.peerApplier == nil {
		return nil
	}

	users := make([]tg.UserClass, 0, len(stored))
	chats := make([]tg.ChatClass, 0, len(stored))

	for _, peer := range stored {
		switch {
		case peer.User != nil && !peer.User.Min && peer.Key.AccessHash != 0:
			user := *peer.User
			user.AccessHash = peer.Key.AccessHash
			users = append(users, &user)
		case peer.Chat != nil:
			chats = append(chats, peer.Chat)
		case peer.Channel != nil && !peer.Channel.Min && peer.Key.AccessHash != 0:
			channel := *peer.Channel
			channel.AccessHash = peer.Key.AccessHash
			chats = append(chats, &channel)
		}
	}

	return c.peerApplier.Apply(ctx, users, chats)
}

// syncPeerManager reloads bbolt instead of applying the possibly stale cache
// snapshot created before authentication.
func (c *Client) syncPeerManager(ctx context.Context) error {
	if c.dialogCache == nil {
		return nil
	}

	return c.peerSync.Do(func() error {
		stored, err := c.dialogCache.canonicalSnapshot(ctx)
		if err != nil {
			return err
		}

		c.dialogCache.replaceMemory(stored)

		return c.applyStoredPeers(ctx, stored)
	})
}
