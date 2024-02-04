package telegram

import (
	"context"
	"strings"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type CachedPeerFilter interface {
	filter(CachedPeer) bool
}

type keyKindCachedPeerFilter struct {
	kind dialogs.PeerKind
}

func (f keyKindCachedPeerFilter) filter(p CachedPeer) bool {
	return p.Key.Kind == f.kind
}

func OnlyUsersCachedPeerFilter() CachedPeerFilter {
	return keyKindCachedPeerFilter{kind: dialogs.User}
}

func OnlyChatsCachedPeerFilter() CachedPeerFilter {
	return keyKindCachedPeerFilter{kind: dialogs.Chat}
}

func OnlyChannelsCachedPeerFilter() CachedPeerFilter {
	return keyKindCachedPeerFilter{kind: dialogs.Channel}
}

type nameCachedPeerFilter struct {
	name string
}

// NameCachedPeerFilter returns a filter that matches peers by substring of their name.
func NameCachedPeerFilter(name string) CachedPeerFilter {
	return nameCachedPeerFilter{name: name}
}

func (f nameCachedPeerFilter) filter(p CachedPeer) bool {
	return strings.Contains(strings.ToLower(p.Name()), strings.ToLower(f.name))
}

type not struct {
	f CachedPeerFilter
}

func NotCachedPeerFilter(f CachedPeerFilter) CachedPeerFilter {
	return not{f: f}
}

func (f not) filter(p CachedPeer) bool {
	return !f.f.filter(p)
}

type and struct {
	filters []CachedPeerFilter
}

func AndCachedPeerFilter(filters ...CachedPeerFilter) CachedPeerFilter {
	return and{filters: filters}
}

func (f and) filter(p CachedPeer) bool {
	for _, filter := range f.filters {
		if !filter.filter(p) {
			return false
		}
	}

	return true
}

type or struct {
	filters []CachedPeerFilter
}

func OrCachedPeerFilter(filters ...CachedPeerFilter) CachedPeerFilter {
	return or{filters: filters}
}

func (f or) filter(p CachedPeer) bool {
	for _, filter := range f.filters {
		if filter.filter(p) {
			return true
		}
	}

	return false
}

type CachedPeer struct {
	storage.Peer
}

func (p CachedPeer) Name() string {
	if p.User != nil {
		return p.User.Username
	} else if p.Chat != nil {
		return p.Chat.Title
	} else if p.Channel != nil {
		return p.Channel.Title
	}

	return ""
}

type CacheService interface {
	GetCachedPeers(ctx context.Context, filters ...CachedPeerFilter) ([]CachedPeer, error)
	CacheChat(ctx context.Context, chat tg.ChatClass) error
	CacheUser(ctx context.Context, user tg.UserClass) error
}

type cacheService service

var _ CacheService = (*cacheService)(nil)

func (s *cacheService) GetCachedPeers(ctx context.Context, filters ...CachedPeerFilter) ([]CachedPeer, error) {
	if s.client.storage == nil {
		return nil, errPeerStoreNotSet
	}

	iter, err := s.client.storage.Iterate(ctx)
	if err != nil {
		return nil, err
	}

	peers := make([]CachedPeer, 0)
	storage.ForEach(ctx, iter, func(p storage.Peer) error {
		peer := CachedPeer{p}
		for _, filter := range filters {
			if !filter.filter(peer) {
				return nil
			}
		}

		peers = append(peers, peer)
		return nil
	})

	return peers, nil
}

func (s *cacheService) CacheChat(ctx context.Context, chat tg.ChatClass) error {
	if s.client.storage == nil {
		return errPeerStoreNotSet
	}

	peer := storage.Peer{}
	peer.FromChat(chat)

	return s.client.storage.Add(ctx, peer)
}

func (s *cacheService) CacheUser(ctx context.Context, user tg.UserClass) error {
	if s.client.storage == nil {
		return errPeerStoreNotSet
	}

	peer := storage.Peer{}
	peer.FromUser(user)

	return s.client.storage.Add(ctx, peer)
}
