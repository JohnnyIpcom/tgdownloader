# Dialog Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make dialog listing, prompt suggestions, and name resolution use one fast persistent dialog snapshot with visible names.

**Architecture:** Keep gotd's `peers` bucket as canonical peer data and add a `dialogs` bucket containing only `storage.PeerKey` membership. Load indexed peers into memory once, bootstrap only when empty, refresh from `dialogs.Elem.Entities` without per-peer RPCs, and feed prompt lookups from the memory snapshot.

**Tech Stack:** Go 1.25, gotd/td v0.143.0, gotd/contrib storage, bbolt, Cobra, go-prompt.

## Global Constraints

- Warm startup performs no full dialog RPC and no generic peer scan.
- First empty-cache bootstrap uses `BatchSize(100)` and visible progress.
- User names display `FirstName + LastName`; username is a search alias.
- Prompt suggestions perform no database or network call per keystroke.
- Existing user changes and staged state must be preserved.
- Run `gofmt`; rebuild `tgdownloader.exe` after the fix.
- Do not commit or push before manual user review.
- gotd v0.143.0 has no `OnChannelInaccessible` or account-wide `OnTooLong`; rare removals are corrected by `dialog refresh`.
- `telegram.storage.path` is the primary bbolt path; legacy
  `telegram.cache.path` remains a compatible fallback.

---

### Task 1: Versioned Dialog Index Store

**Files:**
- Create: `pkg/telegram/dialog_cache_store.go`
- Create: `pkg/telegram/dialog_cache_store_test.go`

**Interfaces:**
- Produces: `newDialogCacheStore(db *bolt.DB, peerStorage storage.PeerStorage) (*dialogCacheStore, error)`
- Produces: `load(ctx context.Context) ([]storage.Peer, error)`
- Produces: `replace(ctx context.Context, peers []storage.Peer) error`
- Produces: `upsert(ctx context.Context, peer storage.Peer) error`
- Produces: `remove(ctx context.Context, key storage.PeerKey) error`

- [ ] Write tests proving first open resets legacy `peers`, creates schema version and empty `dialogs`.
- [ ] Run `go test ./pkg/telegram -run DialogCacheStore -count=1`; expect missing symbols.
- [ ] Implement bbolt schema metadata and dialog membership keys. Store peer records through gotd `PeerStorage`; replace membership in one transaction.
- [ ] Add tests for load, all three peer kinds, stale membership removal, and missing referenced peer.
- [ ] Run focused store tests; expect PASS.

### Task 2: In-Memory Dialog Cache And Names

**Files:**
- Modify: `pkg/telegram/cache.go`
- Modify: `pkg/telegram/cache_test.go`

**Interfaces:**
- Produces: `DialogPeer.Name() string` using visible-name semantics.
- Produces: `DialogPeer.SearchNames() []string` containing visible name and username aliases.
- Produces: `dialogCache.ReplaceDialogs(ctx context.Context, peers []storage.Peer) error`
- Produces: `dialogCache.UpsertDialog(ctx context.Context, peer storage.Peer) error`
- Existing: `GetDialogPeers` filters a copied in-memory dialog snapshot.

- [ ] Change tests to require visible name even when username exists; add alias tests.
- [ ] Run focused tests; expect current username-first assertion to fail.
- [ ] Add mutex-protected map keyed by `storage.PeerKey`, backed by `dialogCacheStore`.
- [ ] Test replacement, filters, concurrent reads, and no generic storage iteration.
- [ ] Run cache tests; expect PASS.

### Task 3: Fast Dialog Bootstrap And Refresh

**Files:**
- Modify: `pkg/telegram/dialog.go`
- Modify: `pkg/telegram/dialog_test.go`
- Modify: `internal/renderer/dialog.go`

**Interfaces:**
- `Dialog` embeds `DialogPeer`, not `peers.Peer`.
- Produces: `dialogPeer(elem dialogs.Elem) (storage.Peer, bool)` using `Elem.Entities`.
- Existing `GetAllDialogs` fetches batches, streams cached peers, then atomically replaces cache on success.

- [ ] Write conversion tests for user, chat, and channel entities and malformed elements.
- [ ] Run focused tests; expect missing conversion function.
- [ ] Replace `peerMgr.FromInputPeer` with direct entity conversion.
- [ ] Ensure canceled/failed iteration never replaces previous snapshot.
- [ ] Adapt dialog renderer to `DialogPeer.Name()`, ID, TDLib ID, and type.
- [ ] Run dialog and renderer tests; expect PASS.

### Task 4: Lifecycle Bootstrap And Incremental Updates

**Files:**
- Modify: `pkg/telegram/client.go`
- Modify: `pkg/telegram/client_test.go`
- Create: `pkg/telegram/dialog_updates.go`
- Create: `pkg/telegram/dialog_updates_test.go`

**Interfaces:**
- Client creation initializes schema/store and loads warm snapshot locally.
- Empty snapshot is bootstrapped inside updates `OnStart` with a `Dialog cache` tracker.
- Dispatcher handlers upsert peers from `UpdateNewMessage`, `UpdateNewChannelMessage`, `UpdateUser`, `UpdateChat`, and `UpdateChannel` entities.

- [ ] Write tests for warm startup skipping bootstrap and empty startup requesting it.
- [ ] Run tests; expect missing bootstrap decision logic.
- [ ] Wire store/cache before service initialization and expose cache size/empty check.
- [ ] Register update handlers and test direct user, chat, and channel additions.
- [ ] Keep current update-start timeout/error handling; surface bootstrap failure before prompt starts.
- [ ] Run lifecycle/update tests; expect PASS.

### Task 5: Unified Prompt Search And Resolution

**Files:**
- Modify: `cmd/cmd/helpers.go`
- Modify: `cmd/cmd/prompt.go`
- Modify: `cmd/cmd/peer_input_test.go`

**Interfaces:**
- Suggestions and resolution consume `DialogCache.GetDialogPeers`, the dialog snapshot.
- Name matching checks sanitized `DialogPeer.SearchNames()`; suggestion text remains sanitized visible name.
- TDLib ID matching remains unchanged.

- [ ] Add failing test where visible name `_anastasiia_` is displayed and username `lscptd` also finds the same peer.
- [ ] Run `go test ./cmd/cmd -run Peer -count=1`; expect alias lookup failure.
- [ ] Update suggestion and exact/prefix resolution to evaluate all aliases while displaying `Name()`.
- [ ] Test duplicate visible names, duplicate aliases, multi-word names, long names, symbols, and ID input.
- [ ] Run command tests; expect PASS.

### Task 6: Consolidate Dialog Commands

**Files:**
- Modify: `cmd/cmd/dialogs.go`
- Delete: `cmd/cmd/cache.go`
- Modify: `cmd/cmd/root.go`
- Modify: `internal/renderer/cache.go`
- Test: `cmd/cmd/dialogs_test.go`

**Interfaces:**
- `dialog list` reads `DialogCache.GetDialogPeers` and renders immediately.
- `dialog refresh` drains `DialogService.GetAllDialogs`, then prints `Refreshed N dialogs` after successful cache replacement.
- Root command no longer registers `cache`.

- [x] Add command tests proving `dialog list` reads only the cache and `dialog refresh` reports the refreshed count.
- [x] Run focused command tests; expect failures while old behavior remains.
- [x] Move cached-list rendering under `dialog list`, add `dialog refresh`, and remove `cache` command registration and implementation.
- [x] Remove renderer functions made unused by command consolidation.
- [x] Run focused command tests; expect PASS.

### Task 7: Final Verification And Binary

**Files:**
- Modify mechanically: all changed Go files through `gofmt`.
- Build: `tgdownloader.exe`.

- [x] Run `gofmt` on every changed Go file.
- [x] Run focused package tests for `pkg/telegram` and `cmd/cmd`.
- [x] Run `go test ./...`; expect PASS.
- [x] Run `go build ./...`; expect PASS.
- [x] Run `go build -o tgdownloader.exe .`; expect exit code 0.
- [x] Inspect `git diff`, status, and executable timestamp. Do not stage, commit, or push.
