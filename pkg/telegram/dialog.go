package telegram

import (
	"context"
	"errors"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"go.uber.org/zap"
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

func (s *dialogService) sendDialog(ctx context.Context, out chan<- Dialog, dialog Dialog) error {
	select {
	case out <- dialog:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
			peer, err := s.client.peerMgr.FromInputPeer(ctx, elem.Peer)
			if err != nil {
				return s.sendDialog(ctx, dialogsChan, Dialog{err: err})
			}

			if err := s.client.CacheDialog(ctx, elem); err != nil {
				s.logger.Error("failed to cache dialog", zap.Error(err))
				return nil
			}

			if err := s.sendDialog(ctx, dialogsChan, Dialog{Peer: peer}); err != nil {
				return err
			}
			return nil
		}); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("failed to get dialogs", zap.Error(err))
		}
	}()

	return dialogsChan, count, err
}
