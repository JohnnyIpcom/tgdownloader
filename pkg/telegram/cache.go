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
	Peer       PeerInfo
	AccessHash int64
	CreatedAt  time.Time
}

type PeerCacheInfoFilter interface {
	filter(storage.Peer) bool
}

type keyKindPeerCacheInfoFilter struct {
	kind dialogs.PeerKind
}

func (f keyKindPeerCacheInfoFilter) filter(p storage.Peer) bool {
	return p.Key.Kind == f.kind
}

func OnlyUsersPeerCacheInfoFilter() PeerCacheInfoFilter {
	return keyKindPeerCacheInfoFilter{kind: dialogs.User}
}

func OnlyChatsPeerCacheInfoFilter() PeerCacheInfoFilter {
	return keyKindPeerCacheInfoFilter{kind: dialogs.Chat}
}

func OnlyChannelsPeerCacheInfoFilter() PeerCacheInfoFilter {
	return keyKindPeerCacheInfoFilter{kind: dialogs.Channel}
}

type not struct {
	f PeerCacheInfoFilter
}

func NotPeerCacheInfoFilter(f PeerCacheInfoFilter) PeerCacheInfoFilter {
	return not{f: f}
}

func (f not) filter(p storage.Peer) bool {
	return !f.f.filter(p)
}

type and struct {
	filters []PeerCacheInfoFilter
}

func AndPeerCacheInfoFilter(filters ...PeerCacheInfoFilter) PeerCacheInfoFilter {
	return and{filters: filters}
}

func (f and) filter(p storage.Peer) bool {
	for _, filter := range f.filters {
		if !filter.filter(p) {
			return false
		}
	}

	return true
}

type or struct {
	filters []PeerCacheInfoFilter
}

func OrPeerCacheInfoFilter(filters ...PeerCacheInfoFilter) PeerCacheInfoFilter {
	return or{filters: filters}
}

func (f or) filter(p storage.Peer) bool {
	for _, filter := range f.filters {
		if filter.filter(p) {
			return true
		}
	}

	return false
}

type CacheService interface {
	UpdateDialogCache(ctx context.Context) error
	GetPeersFromCache(ctx context.Context, filters ...PeerCacheInfoFilter) (<-chan PeerCacheInfo, error)
	CollectPeersFromCache(ctx context.Context, filters ...PeerCacheInfoFilter) ([]PeerCacheInfo, error)
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

func (s *cacheService) GetPeersFromCache(ctx context.Context, filters ...PeerCacheInfoFilter) (<-chan PeerCacheInfo, error) {
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
			for _, filter := range filters {
				if !filter.filter(p) {
					return nil
				}
			}

			peerCacheInfo <- getPeerCacheInfoFromStoragePeer(p)
			return nil
		})
	}()

	return peerCacheInfo, nil
}

func (s *cacheService) CollectPeersFromCache(ctx context.Context, filters ...PeerCacheInfoFilter) ([]PeerCacheInfo, error) {
	if s.client.storage == nil {
		return nil, errPeerStoreNotSet
	}

	iter, err := s.client.storage.Iterate(ctx)
	if err != nil {
		return nil, err
	}

	var peers []PeerCacheInfo
	if err := storage.ForEach(ctx, iter, func(p storage.Peer) error {
		for _, filter := range filters {
			if !filter.filter(p) {
				return nil
			}
		}

		peers = append(peers, getPeerCacheInfoFromStoragePeer(p))
		return nil
	}); err != nil {
		return nil, err
	}

	return peers, nil
}
