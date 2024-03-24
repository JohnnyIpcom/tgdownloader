package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
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
	peer, err := c.client.peerMgr.ResolveTDLibID(ctx, ID)
	if err != nil {
		return nil, err
	}

	return peer, nil
}
