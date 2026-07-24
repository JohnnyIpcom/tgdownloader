package telegram

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/gotd/contrib/storage"
	bolt "go.etcd.io/bbolt"
)

var (
	dialogCacheBucket      = []byte("dialogs")
	dialogCacheMetadata    = []byte("dialog_cache_metadata")
	dialogCacheSchemaKey   = []byte("schema_version")
	dialogCachePeersBucket = []byte("peers")
)

const dialogCacheSchemaVersion uint64 = 1

type dialogCacheStore struct {
	db          *bolt.DB
	peerStorage storage.PeerStorage
}

func newDialogCacheStore(db *bolt.DB, peerStorage storage.PeerStorage) (*dialogCacheStore, error) {
	store := &dialogCacheStore{db: db, peerStorage: peerStorage}
	if err := db.Update(func(tx *bolt.Tx) error {
		metadata, err := tx.CreateBucketIfNotExists(dialogCacheMetadata)
		if err != nil {
			return fmt.Errorf("create dialog cache metadata: %w", err)
		}

		version := metadata.Get(dialogCacheSchemaKey)
		if len(version) != 8 || binary.LittleEndian.Uint64(version) != dialogCacheSchemaVersion {
			if err := tx.DeleteBucket(dialogCachePeersBucket); err != nil && err != bolt.ErrBucketNotFound {
				return fmt.Errorf("reset peer cache: %w", err)
			}
			if err := tx.DeleteBucket(dialogCacheBucket); err != nil && err != bolt.ErrBucketNotFound {
				return fmt.Errorf("reset dialog cache: %w", err)
			}
		}

		if _, err := tx.CreateBucketIfNotExists(dialogCachePeersBucket); err != nil {
			return fmt.Errorf("create peer cache: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(dialogCacheBucket); err != nil {
			return fmt.Errorf("create dialog cache: %w", err)
		}

		encodedVersion := make([]byte, 8)
		binary.LittleEndian.PutUint64(encodedVersion, dialogCacheSchemaVersion)
		if err := metadata.Put(dialogCacheSchemaKey, encodedVersion); err != nil {
			return fmt.Errorf("write dialog cache schema: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *dialogCacheStore) load(ctx context.Context) ([]storage.Peer, error) {
	var keys []storage.PeerKey
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(dialogCacheBucket)
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, _ []byte) error {
			var key storage.PeerKey
			if err := key.Parse(k); err != nil {
				return fmt.Errorf("parse dialog cache key: %w", err)
			}
			keys = append(keys, key)
			return nil
		})
	}); err != nil {
		return nil, err
	}

	peers := make([]storage.Peer, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		peer, err := s.peerStorage.Find(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("load dialog peer %s: %w", key.String(), err)
		}
		peers = append(peers, peer)
	}

	return peers, nil
}

func (s *dialogCacheStore) replace(ctx context.Context, peers []storage.Peer) error {
	for _, peer := range peers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.peerStorage.Add(ctx, peer); err != nil {
			return fmt.Errorf("store dialog peer %s: %w", storage.KeyFromPeer(peer).String(), err)
		}
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(dialogCacheBucket); err != nil && err != bolt.ErrBucketNotFound {
			return fmt.Errorf("replace dialog cache: %w", err)
		}
		bucket, err := tx.CreateBucket(dialogCacheBucket)
		if err != nil {
			return fmt.Errorf("create replacement dialog cache: %w", err)
		}
		for _, peer := range peers {
			if err := bucket.Put(storage.KeyFromPeer(peer).Bytes(nil), nil); err != nil {
				return fmt.Errorf("index dialog peer: %w", err)
			}
		}
		return nil
	})
}

func (s *dialogCacheStore) upsert(ctx context.Context, peer storage.Peer) error {
	if err := s.peerStorage.Add(ctx, peer); err != nil {
		return fmt.Errorf("store dialog peer: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(dialogCacheBucket)
		if err != nil {
			return fmt.Errorf("create dialog cache: %w", err)
		}
		return bucket.Put(storage.KeyFromPeer(peer).Bytes(nil), nil)
	})
}

func (s *dialogCacheStore) remove(_ context.Context, key storage.PeerKey) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(dialogCacheBucket)
		if bucket == nil {
			return nil
		}
		return bucket.Delete(key.Bytes(nil))
	})
}
