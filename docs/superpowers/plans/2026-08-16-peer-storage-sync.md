# Peer Storage Synchronization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep gotd's runtime peer manager synchronized with the durable bbolt peer store and prevent zero-access-hash resolution after restart.

**Architecture:** bbolt remains canonical for full persisted entities and dialog membership. A deferred update-handler bridge installs gotd's manager hook once the Telegram API client exists; startup and dialog refresh explicitly apply persisted entities to the manager. TDLib ID resolution uses complete persisted entities before falling back to gotd's network resolver.

**Tech Stack:** Go 1.25+, gotd/td peers and updates, gotd/contrib bbolt peer storage, bbolt.

## Global Constraints

- Preserve the current bbolt schema and existing `storage.db`.
- Do not perform a full dialog refresh on every startup.
- Never synthesize an access hash.
- Keep dialog suggestions backed by the existing in-memory dialog map.
- Run `gofmt` on every changed Go file.
- Rebuild `tgdownloader.exe` before manual review.
- Do not commit before manual review.

---

### Task 1: Persisted TDLib Resolution

**Files:**
- Modify: `pkg/telegram/peer.go`
- Create: `pkg/telegram/peer_test.go`

**Interfaces:**
- Produces: `storedPeerKey(constant.TDLibPeerID) (storage.PeerKey, bool)`.
- Produces: `(*peerService).resolveStoredTDLibID(context.Context, constant.TDLibPeerID) (peers.Peer, bool, error)`.

- [x] Write a failing test with a persisted user whose access hash is nonzero and an otherwise empty gotd manager.
- [x] Assert the resolved peer's `InputPeer` retains the persisted hash.
- [x] Add channel and missing/incomplete entity cases.
- [x] Run `go test ./pkg/telegram -run 'TestPeerServiceResolveStored' -count=1` and confirm failure.
- [x] Implement persisted-first resolution and manager fallback.
- [x] Run the focused tests and confirm success.

### Task 2: Startup And Refresh Synchronization

**Files:**
- Modify: `pkg/telegram/client.go`
- Modify: `pkg/telegram/dialog.go`
- Modify: `pkg/telegram/cache.go`
- Create or modify tests under: `pkg/telegram`

**Interfaces:**
- Produces: `peerEntityApplier` with `Apply(context.Context, []tg.UserClass, []tg.ChatClass) error`.
- Produces: `(*Client).applyStoredPeers(context.Context, []storage.Peer) error`.
- Produces: `(*dialogCache).snapshot() []storage.Peer`.

- [x] Write failing tests that convert cached users/chats/channels into one manager `Apply` call.
- [x] Write a failing startup-sync test using an empty runtime manager and populated dialog cache.
- [x] Write a failing dialog-refresh test proving gathered entities are merged and applied consistently.
- [x] Implement entity conversion, canonical cache reload, startup seed after auth, and refresh synchronization.
- [x] Run focused synchronization tests.

### Task 3: Dual Update Hook

**Files:**
- Modify: `pkg/telegram/client.go`
- Create: `pkg/telegram/peer_update_handler.go`
- Create: `pkg/telegram/peer_update_handler_test.go`

**Interfaces:**
- Produces: a concurrency-safe deferred `telegram.UpdateHandler` whose delegate can be replaced before client startup.

- [x] Write failing tests proving the initial handler delegates to bbolt/dispatcher flow and the installed manager hook receives later updates.
- [x] Implement the deferred handler with an `RWMutex`, shared synchronization coordinator, and nil-safe delegate replacement.
- [x] Build `updates.Manager` with the bridge, then install `peerMgr.UpdateHook(baseHandler)` after creating `peers.Manager`.
- [x] Run focused handler tests and `go test -race ./pkg/telegram`.

### Task 4: Verification And Executable

**Files:**
- Modify only if verification finds a defect.

- [x] Run `gofmt` on changed Go files.
- [x] Run `go test ./...`.
- [x] Run `go vet ./...`.
- [x] Run `git diff --check`.
- [x] Build `tgdownloader.exe`.
- [ ] Stop for manual verification of `download history _anastasiia_`; do not commit.
