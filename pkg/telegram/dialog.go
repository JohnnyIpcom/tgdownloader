package telegram

import (
	"context"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
)

type DialogInfo struct {
	Peer PeerInfo
	err  error
}

func (d DialogInfo) Err() error {
	return d.err
}

type DialogClient interface {
	GetAllDialogs(ctx context.Context) (<-chan DialogInfo, int, error)
}

var _ DialogClient = (*client)(nil)

func (c *client) GetAllDialogs(ctx context.Context) (<-chan DialogInfo, int, error) {
	queryBuilder := query.GetDialogs(c.client.API())
	queryBuilder.BatchSize(100)

	dialogsChan := make(chan DialogInfo)

	count, err := queryBuilder.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	go func() {
		defer close(dialogsChan)

		queryBuilder.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			if err := c.storeDialog(ctx, elem); err != nil {
				return err
			}

			peer, err := c.peerMgr.FromInputPeer(ctx, elem.Peer)
			if err != nil {
				dialogsChan <- DialogInfo{err: err}
				return nil
			}

			dialogsChan <- DialogInfo{Peer: c.getInfoFromPeer(peer)}
			return nil
		})
	}()

	return dialogsChan, count, err
}
