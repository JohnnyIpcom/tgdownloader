package telegram

import (
	"context"
	"encoding/binary"

	"github.com/gotd/td/telegram/updates"
	bolt "go.etcd.io/bbolt"
)

var channelAccessHashBucket = []byte("updates_channel_access_hash")

type boltChannelAccessHasher struct {
	db *bolt.DB
}

var _ updates.ChannelAccessHasher = (*boltChannelAccessHasher)(nil)

func newBoltChannelAccessHasher(db *bolt.DB) *boltChannelAccessHasher {
	return &boltChannelAccessHasher{db: db}
}

func (h *boltChannelAccessHasher) SetChannelAccessHash(_ context.Context, userID, channelID, accessHash int64) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(channelAccessHashBucket)
		if err != nil {
			return err
		}

		return b.Put(channelAccessHashKey(userID, channelID), int64Bytes(accessHash))
	})
}

func (h *boltChannelAccessHasher) GetChannelAccessHash(_ context.Context, userID, channelID int64) (int64, bool, error) {
	var (
		hash  int64
		found bool
	)

	err := h.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(channelAccessHashBucket)
		if b == nil {
			return nil
		}

		v := b.Get(channelAccessHashKey(userID, channelID))
		if v == nil {
			return nil
		}

		hash = bytesInt64(v)
		found = true
		return nil
	})
	if err != nil {
		return 0, false, err
	}

	return hash, found, nil
}

func channelAccessHashKey(userID, channelID int64) []byte {
	key := make([]byte, 16)
	binary.LittleEndian.PutUint64(key[:8], uint64(userID))
	binary.LittleEndian.PutUint64(key[8:], uint64(channelID))
	return key
}

func int64Bytes(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func bytesInt64(b []byte) int64 {
	return int64(binary.LittleEndian.Uint64(b))
}
