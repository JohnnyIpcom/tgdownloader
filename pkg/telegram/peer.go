package telegram

import (
	"context"
	"errors"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/gotd-contrib/storage"
)

type PeerType int

func (t PeerType) String() string {
	switch t {
	case PeerTypeChat:
		return "Chat"
	case PeerTypeChannel:
		return "Channel"
	case PeerTypeUser:
		return "User"
	default:
		return "Unknown"
	}
}

const (
	PeerTypeChat PeerType = iota
	PeerTypeChannel
	PeerTypeUser
)

type PeerInfo struct {
	Type PeerType
	ID   int64
	Name string
}

type PeerClient interface {
	GetAllPeers(ctx context.Context) ([]PeerInfo, error)
	PeerSelf(ctx context.Context) (PeerInfo, error)
	FindPeer(ctx context.Context, from string) (PeerInfo, error)
}

var _ PeerClient = (*client)(nil)

func (c *client) getInfoFromPeer(peer peers.Peer) PeerInfo {
	var peerType PeerType
	switch peer.(type) {
	case peers.User:
		peerType = PeerTypeUser
	case peers.Chat:
		peerType = PeerTypeChat
	case peers.Channel:
		peerType = PeerTypeChannel
	}

	return PeerInfo{
		Type: peerType,
		ID:   peer.ID(),
		Name: peer.VisibleName(),
	}
}

// GetAllPeers returns all peers.
func (c *client) GetAllPeers(ctx context.Context) ([]PeerInfo, error) {
	chats, err := c.peerMgr.GetAllChats(ctx)
	if err != nil {
		return nil, err
	}

	var result []PeerInfo
	for _, chat := range chats.Chats {
		result = append(result, c.getInfoFromPeer(chat))
	}

	for _, channel := range chats.Channels {
		result = append(result, c.getInfoFromPeer(channel))
	}

	return result, nil
}

func (c *client) PeerSelf(ctx context.Context) (PeerInfo, error) {
	user, err := c.peerMgr.Self(ctx)
	if err != nil {
		return PeerInfo{}, err
	}

	return c.getInfoFromPeer(user), nil
}

func (c *client) FindPeer(ctx context.Context, from string) (PeerInfo, error) {
	peer, err := c.peerMgr.Resolve(ctx, from)
	if err != nil {
		return PeerInfo{}, err
	}

	return c.getInfoFromPeer(peer), nil
}

func (c *client) getInputPeer(ctx context.Context, ID int64) (tg.InputPeerClass, error) {
	TDLibPeerID := constant.TDLibPeerID(ID)

	if c.peerStore != nil {
		var peerClass tg.PeerClass
		switch {
		case TDLibPeerID.IsUser():
			peerClass = &tg.PeerUser{UserID: ID}

		case TDLibPeerID.IsChat():
			peerClass = &tg.PeerChat{ChatID: ID}

		case TDLibPeerID.IsChannel():
			peerClass = &tg.PeerChannel{ChannelID: ID}

		default:
			return nil, errors.New("unknown peer type")
		}

		if peer, err := storage.FindPeer(ctx, c.peerStore, peerClass); err == nil {
			return peer.AsInputPeer(), nil
		} else {
			if !errors.Is(err, storage.ErrPeerNotFound) {
				return nil, err
			}
		}
	}

	peer, err := c.peerMgr.ResolveTDLibID(ctx, TDLibPeerID)
	if err != nil {
		return nil, err
	}

	return peer.InputPeer(), nil
}
