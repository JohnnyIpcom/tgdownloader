# Peer Storage Synchronization Design

## Problem

Telegram peer data currently exists in three places:

- bbolt `storage.PeerStorage`, which persists full peer entities;
- `dialogCache.peers`, which is restored from bbolt and serves fast dialog
  suggestions;
- gotd `peers.Manager.InmemoryStorage`, which stores access hashes used by API
  peer resolution.

The dialog cache and bbolt store are synchronized, but `peers.Manager` is not
seeded from either one and its `UpdateHook` is not installed. After restart,
resolving a cached dialog by TDLib ID can therefore send an `InputUser` or
`InputChannel` with a zero access hash even though bbolt contains the correct
value.

## Design

bbolt remains the canonical durable peer store. The existing dialog cache
remains the searchable in-memory dialog index. `peers.Manager` remains gotd's
runtime API resolver, but receives the same entities through explicit
synchronization points.

### Startup

After authentication and before runtime commands become available, collect the
dialog keys already loaded into `dialogCache.peers`, reload their current full
entities from bbolt, and call `peers.Manager.Apply`. This seeds gotd's in-memory
access-hash storage without a network-wide dialog refresh and without applying
a stale snapshot captured before authentication.

### Updates

Install both hooks in the Telegram update chain:

1. gotd `peers.Manager.UpdateHook` updates manager access hashes and entity
   state;
2. contrib `storage.UpdateHook` persists full entities to bbolt;
3. the existing dispatcher updates dialog membership and visible names.

Both hooks see every update containing peer entities.

A shared coordinator serializes hook execution with startup synchronization and
dialog-refresh commits. The durable storage wrapper preserves complete entities
when a minimal update arrives. Monotonic in-memory revision tokens distinguish
live updates from an older refresh without relying on bbolt's timestamp
precision. Dialog-cache writes reload the merged canonical entity before
updating the visible in-memory index.

### Dialog Refresh

Before `GetAllDialogs` starts pagination, capture peer-data and dialog-membership
revision tokens. After it gathers dialog entities, merge them into bbolt while
preserving peers, added dialogs, and removed dialogs changed after those tokens.
Reload the canonical entities, replace the dialog index, and apply those same
entities to `peers.Manager`. This keeps an explicit `dialog refresh` immediately
visible to both stores without letting an older paginated result overwrite a
newer live update.

### Resolution Fallback

`PeerService.ResolveTDLibID` first looks up the exact peer in persisted
`storage.PeerStorage` and constructs a managed peer from the stored full entity.
If no complete persisted entity exists, it uses gotd's normal manager resolver.
This protects commands from startup ordering problems or an entity missed by an
update without first issuing a request with a zero access hash.

The fallback never invents an access hash. Missing or incomplete persisted
entities preserve the original manager error.

## Tests

- Startup sync copies persisted user and channel access hashes into an empty
  gotd manager.
- Minimal and stale entities do not overwrite complete canonical entities.
- Update handling feeds both manager state and persistent peer storage.
- Dialog refresh applies gathered entities to the manager.
- TDLib resolution falls back to a persisted user with a nonzero access hash.
- Missing or incomplete persisted peers return the original resolution error.
