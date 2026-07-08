package telegram

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openUpdateStorageTestDB(t *testing.T) *bolt.DB {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "storage.db"), 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})

	return db
}

func TestBoltChannelAccessHasherMissHitAndOverwrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	hasher := newBoltChannelAccessHasher(openUpdateStorageTestDB(t))

	hash, found, err := hasher.GetChannelAccessHash(ctx, 777, 42)
	if err != nil {
		t.Fatalf("miss returned error: %v", err)
	}
	if found {
		t.Fatalf("miss found hash %d", hash)
	}

	if err := hasher.SetChannelAccessHash(ctx, 777, 42, 12345); err != nil {
		t.Fatalf("set first hash: %v", err)
	}

	hash, found, err = hasher.GetChannelAccessHash(ctx, 777, 42)
	if err != nil {
		t.Fatalf("hit returned error: %v", err)
	}
	if !found || hash != 12345 {
		t.Fatalf("hit = (%d, %v), want (12345, true)", hash, found)
	}

	if err := hasher.SetChannelAccessHash(ctx, 777, 42, 67890); err != nil {
		t.Fatalf("overwrite hash: %v", err)
	}

	hash, found, err = hasher.GetChannelAccessHash(ctx, 777, 42)
	if err != nil {
		t.Fatalf("overwrite hit returned error: %v", err)
	}
	if !found || hash != 67890 {
		t.Fatalf("overwrite hit = (%d, %v), want (67890, true)", hash, found)
	}
}
