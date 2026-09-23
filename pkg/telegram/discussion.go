package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// A private comment URL identifies only the discussion group. Telegram may
// omit that group from dialogs when the user hasn't joined it. Accessible
// broadcast channels expose the missing entity through getFullChannel.
func (s *peerService) resolveDiscussionChannel(ctx context.Context, id int64) (result peers.Peer, found bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	var startedRevision uint64
	if revisioned, ok := s.client.storage.(peerRevisionStorage); ok {
		startedRevision = revisioned.Revision()
	}

	if s.client.progress != nil {
		tracker := s.client.progress.Tracker("Resolving discussion group")
		defer func() {
			if err != nil || !found {
				tracker.Fail()
			} else {
				tracker.Done()
			}
		}()
	}

	// Include the hash in the key so a fresh dialog can repair stale access
	// data. Duplicate pinned dialogs should not cause duplicate API requests.
	checked := make(map[tg.InputChannel]bool)
	try := func(channel *tg.Channel) (peers.Peer, bool, error) {
		if channel == nil || channel.Min || channel.AccessHash == 0 {
			return nil, false, nil
		}

		if channel.ID == id {
			return s.rememberDiscussionChannels(ctx, id, []tg.ChatClass{channel}, startedRevision)
		}

		if !channel.Broadcast || !channel.HasLink {
			return nil, false, nil
		}

		input := tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
		if checked[input] {
			return nil, false, nil
		}
		checked[input] = true

		full, err := s.client.peerMgr.API().ChannelsGetFullChannel(ctx, &input)
		if err != nil {
			// A stale candidate must not prevent checking other channels.
			// Network errors, cancellation and flood waits must still surface.
			if tgerr.Is(err, tg.ErrChannelPrivate, tg.ErrChannelInvalid) {
				return nil, false, nil
			}

			return nil, false, fmt.Errorf("get linked chat for channel %d: %w", channel.ID, err)
		}

		return s.rememberDiscussionChannels(ctx, id, full.Chats, startedRevision)
	}

	// Dialog top messages often name their discussion group in Replies. Use
	// that hint before probing every broadcast channel (which can flood-limit
	// accounts with many dialogs). This scan happens only for unknown peers;
	// it neither delays startup nor replaces the dialog membership snapshot.
	var candidates []*tg.Channel
	iterator := query.GetDialogs(s.client.peerMgr.API()).BatchSize(100).Iter()
	for iterator.Next(ctx) {
		elem := iterator.Value()
		stored, ok := dialogPeer(elem)
		if !ok || stored.Channel == nil {
			continue
		}

		preferred := stored.Channel.ID == id
		if message, ok := elem.Last.(*tg.Message); ok {
			if replies, ok := message.GetReplies(); ok {
				linked, ok := replies.GetChannelID()
				preferred = preferred || (ok && linked == id)
			}
		}

		if preferred {
			if peer, ok, err := try(stored.Channel); err != nil || ok {
				return peer, ok, err
			}
		}

		candidates = append(candidates, stored.Channel)
	}

	if err := iterator.Err(); err != nil {
		return nil, false, err
	}

	for _, channel := range candidates {
		if peer, ok, err := try(channel); err != nil || ok {
			return peer, ok, err
		}
	}

	return nil, false, nil
}

// Keep API-discovered entities durable and consistent with the runtime manager,
// without treating an unjoined discussion group as a dialog membership change.
func (s *peerService) rememberDiscussionChannels(
	ctx context.Context,
	id int64,
	chats []tg.ChatClass,
	startedRevision uint64,
) (peers.Peer, bool, error) {
	var target *tg.Channel

	err := s.client.peerSync.Do(func() error {
		stored := make([]storage.Peer, 0, len(chats))
		for _, chat := range chats {
			channel, ok := chat.(*tg.Channel)
			if !ok || channel.Min || channel.AccessHash == 0 {
				continue
			}

			var peer storage.Peer
			peer.FromChat(channel)

			if s.client.storage != nil {
				// Treat discovery as a refresh: live updates received during
				// the network scan take precedence over equally complete data.
				var err error
				if refresh, ok := s.client.storage.(refreshPeerStorage); ok {
					err = refresh.AddRefresh(ctx, peer, startedRevision)
				} else {
					err = s.client.storage.Add(ctx, peer)
				}
				if err != nil {
					return err
				}

				// The coherent store may preserve a newer entity from updates.
				canonical, err := s.client.storage.Find(ctx, storage.KeyFromPeer(peer))
				if err != nil {
					return err
				}
				peer = canonical
			}

			stored = append(stored, peer)
			if channel.ID == id && peer.Channel != nil {
				copy := *peer.Channel
				copy.AccessHash = peer.Key.AccessHash
				target = &copy
			}
		}

		return s.client.applyStoredPeers(ctx, stored)
	})
	if err != nil {
		return nil, false, fmt.Errorf("save discussion peers: %w", err)
	}

	if target != nil {
		return s.client.peerMgr.Channel(target), true, nil
	}

	return nil, false, nil
}
