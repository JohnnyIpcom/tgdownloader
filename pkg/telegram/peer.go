package telegram

import (
	"context"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
)

type PeerService interface {
	Self(ctx context.Context) (peers.Peer, error)
	Resolve(ctx context.Context, from string) (peers.Peer, error)
	ResolveTDLibID(ctx context.Context, ID constant.TDLibPeerID) (peers.Peer, error)
}

type peerService service

var _ PeerService = (*peerService)(nil)

func (s *peerService) Self(ctx context.Context) (peers.Peer, error) {
	user, err := s.client.peerMgr.Self(ctx)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *peerService) Resolve(ctx context.Context, from string) (peers.Peer, error) {
	peer, err := s.client.peerMgr.Resolve(ctx, from)
	if err != nil {
		return nil, err
	}

	return peer, nil
}

func (c *peerService) ResolveTDLibID(ctx context.Context, ID constant.TDLibPeerID) (peers.Peer, error) {
	peer, err := c.client.peerMgr.ResolveTDLibID(ctx, ID)
	if err != nil {
		return nil, err
	}

	return peer, nil
}
