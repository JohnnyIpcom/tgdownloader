package telegram

import (
	"context"
	"errors"
	"sync"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/query/dialogs"
)

// coherentPeerStorage prevents partial or stale entities from degrading the
// canonical bbolt record while keeping the contrib storage interface intact.
type coherentPeerStorage struct {
	storage.PeerStorage
	mu        sync.Mutex
	revision  uint64
	revisions map[storage.PeerKey]uint64
}

func newCoherentPeerStorage(peerStorage storage.PeerStorage) *coherentPeerStorage {
	return &coherentPeerStorage{
		PeerStorage: peerStorage,
		revisions:   make(map[storage.PeerKey]uint64),
	}
}

func (s *coherentPeerStorage) Add(ctx context.Context, value storage.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, err := s.merge(ctx, value, false)
	if err != nil {
		return err
	}

	if err := s.PeerStorage.Add(ctx, value); err != nil {
		return err
	}

	s.markUpdated(storage.KeyFromPeer(value))

	return nil
}

func (s *coherentPeerStorage) Assign(ctx context.Context, key string, value storage.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, err := s.merge(ctx, value, false)
	if err != nil {
		return err
	}

	if err := s.PeerStorage.Assign(ctx, key, value); err != nil {
		return err
	}

	s.markUpdated(storage.KeyFromPeer(value))

	return nil
}

func (s *coherentPeerStorage) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.revision
}

// AddRefresh writes a paginated refresh entity unless this peer changed after
// the refresh started. Entity completeness always has higher priority.
func (s *coherentPeerStorage) AddRefresh(
	ctx context.Context,
	value storage.Peer,
	startedRevision uint64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := storage.KeyFromPeer(value)
	value, err := s.merge(ctx, value, s.revisions[key] > startedRevision)
	if err != nil {
		return err
	}

	if err := s.PeerStorage.Add(ctx, value); err != nil {
		return err
	}

	s.markUpdated(key)

	return nil
}

// merge selects one durable entity. Revisions resolve equally complete
// entities; they never let a minimal entity replace a complete one.
func (s *coherentPeerStorage) merge(
	ctx context.Context,
	incoming storage.Peer,
	preserveCurrent bool,
) (storage.Peer, error) {
	current, err := s.PeerStorage.Find(ctx, storage.KeyFromPeer(incoming))
	if errors.Is(err, storage.ErrPeerNotFound) {
		return incoming, nil
	}

	if err != nil {
		return storage.Peer{}, err
	}

	selected, other := incoming, current
	incomingRank := storedPeerCompleteness(incoming)
	currentRank := storedPeerCompleteness(current)

	if currentRank > incomingRank || (preserveCurrent && currentRank == incomingRank) {
		selected, other = current, incoming
	}

	if selected.Key.AccessHash == 0 && storedPeerHashAuthoritative(other) {
		selected.Key.AccessHash = other.Key.AccessHash
	}

	if selected.User != nil && !selected.User.Min && selected.User.AccessHash == 0 {
		user := *selected.User
		user.AccessHash = selected.Key.AccessHash
		selected.User = &user
	}

	if selected.Channel != nil && !selected.Channel.Min && selected.Channel.AccessHash == 0 {
		channel := *selected.Channel
		channel.AccessHash = selected.Key.AccessHash
		selected.Channel = &channel
	}

	return selected, nil
}

func (s *coherentPeerStorage) markUpdated(key storage.PeerKey) {
	s.revision++
	s.revisions[key] = s.revision
}

// storedPeerCompleteness ranks entities independently of their access hash.
// Self and deleted users are complete even when Telegram omits that hash.
func storedPeerCompleteness(peer storage.Peer) int {
	switch peer.Key.Kind {
	case dialogs.User:
		if peer.User == nil {
			return 0
		}

		if peer.User.Min {
			return 1
		}

		return 2

	case dialogs.Chat:
		if peer.Chat == nil {
			return 0
		}

		return 2

	case dialogs.Channel:
		if peer.Channel == nil {
			return 0
		}

		if peer.Channel.Min {
			return 1
		}

		return 2
	}

	return 0
}

// storedPeerHashAuthoritative rejects hashes attached only to minimal entities.
func storedPeerHashAuthoritative(peer storage.Peer) bool {
	if peer.Key.AccessHash == 0 {
		return false
	}

	switch peer.Key.Kind {
	case dialogs.User:
		return peer.User == nil || !peer.User.Min
	case dialogs.Channel:
		return peer.Channel == nil || !peer.Channel.Min
	default:
		return false
	}
}
