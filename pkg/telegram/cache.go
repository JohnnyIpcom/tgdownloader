package telegram

import (
	"context"
	"time"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/johnnyipcom/gotd-contrib/storage"
)

type PeerCacheInfo struct {
	ID         int64
	AccessHash int64
	CreatedAt  time.Time
}

type CacheService interface {
	UpdateDialogCache(ctx context.Context) error
	GetPeersFromCache(ctx context.Context) (<-chan PeerCacheInfo, error)
}

type cacheService service

func (s *cacheService) UpdateDialogCache(ctx context.Context) error {
	if s.client.storage == nil {
		return errPeerStoreNotSet
	}

	query := query.GetDialogs(s.client.API())
	return query.ForEach(ctx, func(ctx context.Context, elem dialogs.Elem) error {
		return s.client.storeDialog(ctx, elem)
	})
}

func (s *cacheService) GetPeersFromCache(ctx context.Context) (<-chan PeerCacheInfo, error) {
	if s.client.storage == nil {
		return nil, errPeerStoreNotSet
	}

	iter, err := s.client.storage.Iterate(ctx)
	if err != nil {
		return nil, err
	}

	peerCacheInfo := make(chan PeerCacheInfo)
	go func() {
		defer close(peerCacheInfo)

		storage.ForEach(ctx, iter, func(p storage.Peer) error {
			peerCacheInfo <- PeerCacheInfo{
				ID:         p.Key.ID,
				AccessHash: p.Key.AccessHash,
				CreatedAt:  time.Unix(p.CreatedAt, 0),
			}

			return nil
		})
	}()

	return peerCacheInfo, nil
}
