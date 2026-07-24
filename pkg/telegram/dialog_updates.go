package telegram

import (
	"context"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func registerDialogCacheHandlers(dispatcher tg.UpdateDispatcher, cache *dialogCache, log *zap.Logger) {
	upsertMessagePeer := func(ctx context.Context, entities tg.Entities, message tg.MessageClass) {
		msg, ok := message.AsNotEmpty()
		if !ok {
			return
		}
		peer, ok := dialogPeerFromUpdate(entities, msg.GetPeerID())
		if !ok {
			return
		}
		if err := cache.UpsertDialog(ctx, peer); err != nil {
			log.Warn("failed to update dialog cache from message", zap.Error(err))
		}
	}

	dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		upsertMessagePeer(ctx, entities, update.Message)
		return nil
	})
	dispatcher.OnNewChannelMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewChannelMessage) error {
		upsertMessagePeer(ctx, entities, update.Message)
		return nil
	})

	refreshKnown := func(ctx context.Context, entities tg.Entities, peerID tg.PeerClass) {
		peer, ok := dialogPeerFromUpdate(entities, peerID)
		if !ok {
			return
		}
		key := storage.KeyFromPeer(peer)
		if _, exists := cache.dialog(key); !exists {
			return
		}
		if err := cache.UpsertDialog(ctx, peer); err != nil {
			log.Warn("failed to refresh cached dialog", zap.Error(err))
		}
	}

	dispatcher.OnUser(func(ctx context.Context, entities tg.Entities, update *tg.UpdateUser) error {
		refreshKnown(ctx, entities, &tg.PeerUser{UserID: update.UserID})
		return nil
	})
	dispatcher.OnChat(func(ctx context.Context, entities tg.Entities, update *tg.UpdateChat) error {
		refreshKnown(ctx, entities, &tg.PeerChat{ChatID: update.ChatID})
		return nil
	})
	dispatcher.OnChannel(func(ctx context.Context, entities tg.Entities, update *tg.UpdateChannel) error {
		refreshKnown(ctx, entities, &tg.PeerChannel{ChannelID: update.ChannelID})
		return nil
	})

	dispatcher.OnUserName(func(ctx context.Context, _ tg.Entities, update *tg.UpdateUserName) error {
		key := storage.PeerKey{Kind: dialogs.User, ID: update.UserID}
		cached, ok := cache.dialog(key)
		if !ok || cached.User == nil {
			return nil
		}

		peer := cached.Peer
		user := *peer.User
		user.FirstName = update.FirstName
		user.LastName = update.LastName
		user.Usernames = update.Usernames
		user.Username = activeUsername(update.Usernames)
		peer.User = &user
		if err := cache.UpsertDialog(ctx, peer); err != nil {
			log.Warn("failed to refresh cached user name", zap.Error(err))
		}
		return nil
	})
}

func dialogPeerFromUpdate(entities tg.Entities, peerID tg.PeerClass) (storage.Peer, bool) {
	var peer storage.Peer
	switch id := peerID.(type) {
	case *tg.PeerUser:
		user, ok := entities.Users[id.UserID]
		if !ok || !peer.FromUser(user) {
			return storage.Peer{}, false
		}
	case *tg.PeerChat:
		chat, ok := entities.Chats[id.ChatID]
		if !ok || !peer.FromChat(chat) {
			return storage.Peer{}, false
		}
	case *tg.PeerChannel:
		channel, ok := entities.Channels[id.ChannelID]
		if !ok || !peer.FromChat(channel) {
			return storage.Peer{}, false
		}
	default:
		return storage.Peer{}, false
	}
	return peer, true
}

func activeUsername(usernames []tg.Username) string {
	for _, username := range usernames {
		if username.Active {
			return username.Username
		}
	}
	if len(usernames) > 0 {
		return usernames[0].Username
	}
	return ""
}
