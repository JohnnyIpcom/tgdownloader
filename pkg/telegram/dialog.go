package telegram

import (
	"context"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
)

type Dialog struct {
	peers.Peer
	err error
}

func (d Dialog) Err() error {
	return d.err
}

type DialogService interface {
	GetAllDialogs(ctx context.Context) (<-chan Dialog, int, error)
}

type dialogService service

var _ DialogService = (*dialogService)(nil)

func (s *dialogService) GetAllDialogs(ctx context.Context) (<-chan Dialog, int, error) {
	queryBuilder := query.GetDialogs(s.client.API())
	queryBuilder.BatchSize(100)

	dialogsChan := make(chan Dialog)

	count, err := queryBuilder.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	go func() {
		defer close(dialogsChan)

		queryBuilder.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			peer, err := s.client.peerMgr.FromInputPeer(ctx, elem.Peer)
			if err != nil {
				dialogsChan <- Dialog{err: err}
				return nil
			}

			s.client.cacheDialog(ctx, elem)

			dialogsChan <- Dialog{Peer: peer}
			return nil
		})
	}()

	return dialogsChan, count, err
}
