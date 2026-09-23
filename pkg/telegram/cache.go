package telegram

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/query/dialogs"
)

type DialogPeerFilter interface {
	filter(DialogPeer) bool
}

type keyKindDialogPeerFilter struct {
	kind dialogs.PeerKind
}

func (f keyKindDialogPeerFilter) filter(p DialogPeer) bool {
	return p.Key.Kind == f.kind
}

func OnlyUsersDialogPeerFilter() DialogPeerFilter {
	return keyKindDialogPeerFilter{kind: dialogs.User}
}

func OnlyChatsDialogPeerFilter() DialogPeerFilter {
	return keyKindDialogPeerFilter{kind: dialogs.Chat}
}

func OnlyChannelsDialogPeerFilter() DialogPeerFilter {
	return keyKindDialogPeerFilter{kind: dialogs.Channel}
}

type nameDialogPeerFilter struct {
	name string
}

// NameDialogPeerFilter returns a filter that matches peers by substring of their name.
func NameDialogPeerFilter(name string) DialogPeerFilter {
	return nameDialogPeerFilter{name: name}
}

func (f nameDialogPeerFilter) filter(p DialogPeer) bool {
	return strings.Contains(strings.ToLower(p.Name()), strings.ToLower(f.name))
}

type not struct {
	f DialogPeerFilter
}

func NotDialogPeerFilter(f DialogPeerFilter) DialogPeerFilter {
	return not{f: f}
}

func (f not) filter(p DialogPeer) bool {
	return !f.f.filter(p)
}

type and struct {
	filters []DialogPeerFilter
}

func AndDialogPeerFilter(filters ...DialogPeerFilter) DialogPeerFilter {
	return and{filters: filters}
}

func (f and) filter(p DialogPeer) bool {
	for _, filter := range f.filters {
		if !filter.filter(p) {
			return false
		}
	}

	return true
}

type or struct {
	filters []DialogPeerFilter
}

func OrDialogPeerFilter(filters ...DialogPeerFilter) DialogPeerFilter {
	return or{filters: filters}
}

func (f or) filter(p DialogPeer) bool {
	for _, filter := range f.filters {
		if filter.filter(p) {
			return true
		}
	}

	return false
}

type DialogPeer struct {
	storage.Peer
}

func (p DialogPeer) Name() string {
	if p.User != nil {
		if name := strings.TrimSpace(strings.Join([]string{p.User.FirstName, p.User.LastName}, " ")); name != "" {
			return name
		}
		if p.User.Username != "" {
			return p.User.Username
		}
		if p.User.Deleted {
			return "<deleted user>"
		}
		return fmt.Sprintf("<user %d>", p.User.ID)
	} else if p.Chat != nil {
		return p.Chat.Title
	} else if p.Channel != nil {
		return p.Channel.Title
	}

	return ""
}

func (p DialogPeer) SearchNames() []string {
	aliases := []string{p.Name()}
	switch {
	case p.User != nil:
		aliases = append(aliases, p.User.Username)
	case p.Channel != nil:
		aliases = append(aliases, p.Channel.Username)
	}

	result := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, alias) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, alias)
		}
	}
	return result
}

func (p DialogPeer) TDLibPeerID() constant.TDLibPeerID {
	var peerID constant.TDLibPeerID
	switch p.Key.Kind {
	case dialogs.User:
		peerID.User(p.Key.ID)
	case dialogs.Chat:
		peerID.Chat(p.Key.ID)
	case dialogs.Channel:
		peerID.Channel(p.Key.ID)
	}
	return peerID
}

type DialogCache interface {
	GetDialogPeers(ctx context.Context, filters ...DialogPeerFilter) ([]DialogPeer, error)
}

type dialogCache struct {
	mu        sync.RWMutex
	store     *dialogCacheStore
	peers     map[storage.PeerKey]DialogPeer
	revision  uint64
	changedAt map[storage.PeerKey]uint64
	removedAt map[storage.PeerKey]uint64
}

// dialogRefreshToken captures both independently changing parts of the cache.
// It lets a slow dialog refresh preserve live peer and membership updates.
type dialogRefreshToken struct {
	dialogRevision uint64
	peerRevision   uint64
}

// peerRevisionStorage is the optional revision source paired with
// refreshPeerStorage.
type peerRevisionStorage interface {
	Revision() uint64
}

var _ DialogCache = (*dialogCache)(nil)

func newDialogCache(ctx context.Context, store *dialogCacheStore) (*dialogCache, error) {
	peers, err := store.load(ctx)
	if err != nil {
		return nil, err
	}

	s := &dialogCache{
		store:     store,
		peers:     make(map[storage.PeerKey]DialogPeer, len(peers)),
		changedAt: make(map[storage.PeerKey]uint64),
		removedAt: make(map[storage.PeerKey]uint64),
	}

	s.replaceMemory(peers)

	return s, nil
}

func (s *dialogCache) GetDialogPeers(ctx context.Context, filters ...DialogPeerFilter) ([]DialogPeer, error) {
	if ctx == nil {
		return nil, fmt.Errorf("get dialog peers: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	peers := make([]DialogPeer, 0, len(s.peers))
	for _, peer := range s.peers {
		for _, filter := range filters {
			if !filter.filter(peer) {
				goto nextPeer
			}
		}

		peers = append(peers, peer)
	nextPeer:
	}
	s.mu.RUnlock()

	sort.Slice(peers, func(i, j int) bool {
		left := strings.ToLower(peers[i].Name())
		right := strings.ToLower(peers[j].Name())
		if left == right {
			return uint64(peers[i].TDLibPeerID()) < uint64(peers[j].TDLibPeerID())
		}
		return left < right
	})

	return peers, nil
}

func (s *dialogCache) ReplaceDialogs(ctx context.Context, peers []storage.Peer) error {
	if err := s.store.replace(ctx, peers); err != nil {
		return err
	}

	peers, err := s.store.load(ctx)
	if err != nil {
		return err
	}

	s.replaceMemory(peers)

	return nil
}

// ReplaceDialogsFromRefresh merges a paginated snapshot with changes received
// after the refresh started, then reloads the canonical peer data from bbolt.
func (s *dialogCache) ReplaceDialogsFromRefresh(
	ctx context.Context,
	peers []storage.Peer,
	token dialogRefreshToken,
) error {
	peers = s.mergeRefresh(peers, token.dialogRevision)

	if err := s.store.replaceRefresh(ctx, peers, token.peerRevision); err != nil {
		return err
	}

	peers, err := s.store.load(ctx)
	if err != nil {
		return err
	}

	s.replaceMemory(peers)

	return nil
}

func (s *dialogCache) UpsertDialog(ctx context.Context, peer storage.Peer) error {
	if err := s.store.upsert(ctx, peer); err != nil {
		return err
	}

	peer, err := s.store.peerStorage.Find(ctx, storage.KeyFromPeer(peer))
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureRevisionMaps()

	key := storage.KeyFromPeer(peer)
	s.revision++
	s.peers[key] = DialogPeer{Peer: peer}
	s.changedAt[key] = s.revision
	delete(s.removedAt, key)

	return nil
}

func (s *dialogCache) RemoveDialog(ctx context.Context, key storage.PeerKey) error {
	if err := s.store.remove(ctx, key); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureRevisionMaps()

	s.revision++
	delete(s.peers, key)
	delete(s.changedAt, key)
	s.removedAt[key] = s.revision

	return nil
}

func (s *dialogCache) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers) == 0
}

func (s *dialogCache) dialog(key storage.PeerKey) (DialogPeer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[key]
	return peer, ok
}

func (s *dialogCache) snapshot() []storage.Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	peers := make([]storage.Peer, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer.Peer)
	}

	return peers
}

// canonicalSnapshot reloads peer entities from durable storage while retaining
// the in-memory dialog membership index.
func (s *dialogCache) canonicalSnapshot(ctx context.Context) ([]storage.Peer, error) {
	if s.store == nil {
		return s.snapshot(), nil
	}

	return s.store.load(ctx)
}

// startRefresh records the revisions before any network pagination begins.
func (s *dialogCache) startRefresh() dialogRefreshToken {
	s.mu.RLock()
	dialogRevision := s.revision
	s.mu.RUnlock()

	var peerRevision uint64
	if s.store != nil {
		if revisioned, ok := s.store.peerStorage.(peerRevisionStorage); ok {
			peerRevision = revisioned.Revision()
		}
	}

	return dialogRefreshToken{
		dialogRevision: dialogRevision,
		peerRevision:   peerRevision,
	}
}

// mergeRefresh keeps live membership changes. For the same peer, a complete
// refresh entity still wins over a newer minimal entity.
func (s *dialogCache) mergeRefresh(peers []storage.Peer, startedRevision uint64) []storage.Peer {
	next := make(map[storage.PeerKey]storage.Peer, len(peers))
	for _, peer := range peers {
		next[storage.KeyFromPeer(peer)] = peer
	}

	s.mu.RLock()
	for key, peer := range s.peers {
		if s.changedAt[key] > startedRevision {
			refreshed, found := next[key]
			if !found || storedPeerCompleteness(peer.Peer) >= storedPeerCompleteness(refreshed) {
				next[key] = peer.Peer
			}
		}
	}

	for key, removedAt := range s.removedAt {
		if removedAt > startedRevision {
			delete(next, key)
		}
	}
	s.mu.RUnlock()

	merged := make([]storage.Peer, 0, len(next))
	for _, peer := range next {
		merged = append(merged, peer)
	}

	return merged
}

// replaceMemory publishes one canonical snapshot and clears revision markers
// already incorporated into it.
func (s *dialogCache) replaceMemory(peers []storage.Peer) {
	next := make(map[storage.PeerKey]DialogPeer, len(peers))
	for _, peer := range peers {
		next[storage.KeyFromPeer(peer)] = DialogPeer{Peer: peer}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureRevisionMaps()

	s.peers = next
	clear(s.changedAt)
	clear(s.removedAt)
}

func (s *dialogCache) ensureRevisionMaps() {
	if s.changedAt == nil {
		s.changedAt = make(map[storage.PeerKey]uint64)
	}

	if s.removedAt == nil {
		s.removedAt = make(map[storage.PeerKey]uint64)
	}
}
