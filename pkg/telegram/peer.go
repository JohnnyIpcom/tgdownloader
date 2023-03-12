package telegram

import (
	"context"

	"github.com/gotd/td/constant"
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

func (p PeerInfo) TDLibPeerID() constant.TDLibPeerID {
	var id constant.TDLibPeerID
	switch p.Type {
	case PeerTypeChat:
		id.Chat(p.ID)

	case PeerTypeChannel:
		id.Channel(p.ID)

	case PeerTypeUser:
		id.User(p.ID)

	default:
		return 0
	}

	return id
}

type PeerService interface {
	GetAllPeers(ctx context.Context) ([]PeerInfo, error)
	PeerSelf(ctx context.Context) (PeerInfo, error)
	ResolvePeer(ctx context.Context, from string) (PeerInfo, error)
}

type peerService service

var _ PeerService = (*peerService)(nil)

// GetAllPeers returns all peers.
func (s *peerService) GetAllPeers(ctx context.Context) ([]PeerInfo, error) {
	chats, err := s.client.peerMgr.GetAllChats(ctx)
	if err != nil {
		return nil, err
	}

	var result []PeerInfo
	for _, chat := range chats.Chats {
		result = append(result, getInfoFromPeer(chat))
	}

	for _, channel := range chats.Channels {
		result = append(result, getInfoFromPeer(channel))
	}

	return result, nil
}

func (s *peerService) PeerSelf(ctx context.Context) (PeerInfo, error) {
	user, err := s.client.peerMgr.Self(ctx)
	if err != nil {
		return PeerInfo{}, err
	}

	return getInfoFromPeer(user), nil
}

func (s *peerService) ResolvePeer(ctx context.Context, from string) (PeerInfo, error) {
	peer, err := s.client.peerMgr.Resolve(ctx, from)
	if err != nil {
		return PeerInfo{}, err
	}

	return getInfoFromPeer(peer), nil
}
