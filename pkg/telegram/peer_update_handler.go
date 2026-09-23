package telegram

import (
	"context"
	"sync"

	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// deferredUpdateHandler bridges client construction: updates.Manager needs a
// handler before peers.Manager can be built from the Telegram API client.
type deferredUpdateHandler struct {
	mu       sync.RWMutex
	handler  tgclient.UpdateHandler
	peerSync *peerSyncCoordinator
}

// peerSyncCoordinator prevents live updates, startup seeding, and refresh
// commits from applying different peer snapshots concurrently.
type peerSyncCoordinator struct {
	mu sync.Mutex
}

func (c *peerSyncCoordinator) Do(fn func() error) error {
	if c == nil {
		return fn()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return fn()
}

func newDeferredUpdateHandler(
	handler tgclient.UpdateHandler,
	peerSync *peerSyncCoordinator,
) *deferredUpdateHandler {
	return &deferredUpdateHandler{handler: handler, peerSync: peerSync}
}

func (h *deferredUpdateHandler) Set(handler tgclient.UpdateHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handler = handler
}

func (h *deferredUpdateHandler) Handle(ctx context.Context, update tg.UpdatesClass) error {
	h.mu.RLock()
	handler := h.handler
	h.mu.RUnlock()

	if handler == nil {
		return nil
	}

	return h.peerSync.Do(func() error {
		return handler.Handle(ctx, update)
	})
}

var _ tgclient.UpdateHandler = (*deferredUpdateHandler)(nil)
