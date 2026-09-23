package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
)

type PeerService interface {
	Resolve(ctx context.Context, from string) (peers.Peer, error)
	ResolveID(ctx context.Context, ID int64) (peers.Peer, error)
	ResolveTDLibID(ctx context.Context, ID constant.TDLibPeerID) (peers.Peer, error)
}

type peerService service

var _ PeerService = (*peerService)(nil)

func (s *peerService) Resolve(ctx context.Context, from string) (peers.Peer, error) {
	peer, err := s.client.peerMgr.Resolve(ctx, from)
	if err != nil {
		return nil, err
	}

	return peer, nil
}

func (s *peerService) ResolveID(ctx context.Context, ID int64) (peers.Peer, error) {
	var (
		p   peers.Peer
		err error
	)
	if p, err = s.client.peerMgr.ResolveChannelID(ctx, ID); err == nil {
		return p, nil
	}
	if p, err = s.client.peerMgr.ResolveUserID(ctx, ID); err == nil {
		return p, nil
	}
	if p, err = s.client.peerMgr.ResolveChatID(ctx, ID); err == nil {
		return p, nil
	}

	return nil, fmt.Errorf("failed to get result from %d：%v", ID, err)
}

func (c *peerService) ResolveTDLibID(ctx context.Context, ID constant.TDLibPeerID) (peers.Peer, error) {
	if peer, found, err := c.resolveStoredTDLibID(ctx, ID); err != nil {
		return nil, err
	} else if found {
		return peer, nil
	}

	peer, err := c.client.peerMgr.ResolveTDLibID(ctx, ID)
	if err != nil {
		var notFound *peers.PeerNotFoundError
		if ID.IsChannel() && errors.As(err, &notFound) {
			if discussion, found, discoverErr := c.resolveDiscussionChannel(ctx, ID.ToPlain()); discoverErr != nil {
				return nil, fmt.Errorf("discover discussion channel %d: %w", ID.ToPlain(), discoverErr)
			} else if found {
				return discussion, nil
			}
		}

		return nil, err
	}

	return peer, nil
}

// resolveStoredTDLibID uses the complete durable entity before gotd attempts a
// network lookup. This avoids constructing InputUser or InputChannel with a
// zero access hash after restart.
func (c *peerService) resolveStoredTDLibID(
	ctx context.Context,
	peerID constant.TDLibPeerID,
) (peers.Peer, bool, error) {
	if c.client.storage == nil || c.client.peerMgr == nil {
		return nil, false, nil
	}

	key, ok := storedPeerKey(peerID)
	if !ok {
		return nil, false, nil
	}

	stored, err := c.client.storage.Find(ctx, key)
	if errors.Is(err, storage.ErrPeerNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("find persisted peer %s: %w", key.String(), err)
	}

	switch key.Kind {
	case dialogs.User:
		if stored.User == nil || stored.User.Min || (!stored.User.Self && stored.Key.AccessHash == 0) {
			return nil, false, nil
		}

		user := *stored.User
		user.AccessHash = stored.Key.AccessHash

		return c.client.peerMgr.User(&user), true, nil

	case dialogs.Chat:
		if stored.Chat == nil {
			return nil, false, nil
		}

		chat := *stored.Chat

		return c.client.peerMgr.Chat(&chat), true, nil

	case dialogs.Channel:
		if stored.Channel == nil || stored.Channel.Min || stored.Key.AccessHash == 0 {
			return nil, false, nil
		}

		channel := *stored.Channel
		channel.AccessHash = stored.Key.AccessHash

		return c.client.peerMgr.Channel(&channel), true, nil
	}

	return nil, false, nil
}

func storedPeerKey(peerID constant.TDLibPeerID) (storage.PeerKey, bool) {
	key := storage.PeerKey{ID: peerID.ToPlain()}

	switch {
	case peerID.IsUser():
		key.Kind = dialogs.User
	case peerID.IsChat():
		key.Kind = dialogs.Chat
	case peerID.IsChannel():
		key.Kind = dialogs.Channel
	default:
		return storage.PeerKey{}, false
	}

	return key, true
}
