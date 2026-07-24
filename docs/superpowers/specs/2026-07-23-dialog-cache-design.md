# Dialog Cache Design

## Goal

Provide immediate prompt suggestions and make `dialog list`, peer-name suggestions,
and peer-name resolution use the same dialog set and naming rules.

## Storage Model

- Configure the shared bbolt database through `telegram.storage.path`.
  `telegram.cache.path` remains a legacy fallback so existing configurations
  continue to use the same database file.
- Keep gotd's `peers` bucket as the canonical store for peer data and resolver
  aliases.
- Add a separate persistent `dialogs` bucket containing only `storage.PeerKey`
  membership entries for current dialogs.
- Load the indexed peer records into an in-memory map keyed by peer kind and ID
  once when the Telegram client is created.
- UI commands must not scan the generic `peers` bucket.

The `dialogs` index is the source of truth for membership; the referenced
`peers` record is the only source of names, usernames, and access hashes. The
in-memory dialog snapshot makes lookups immediate without duplicating persistent
peer data.

## Startup

- A non-empty dialog cache is loaded locally before connecting. This must not
  perform Telegram RPCs or delay the existing connection lifecycle materially.
- The existing updates manager reconciles changes through persisted update state
  and `getDifference`.
- A full dialog bootstrap runs only when the dialog cache is empty, such as the
  first run after this migration.
- Bootstrap progress remains visible until completion.

## Bootstrap And Refresh

- Fetch dialogs with `BatchSize(100)`.
- Build cached peers directly from each `dialogs.Elem.Entities` collection.
- Do not call `peerMgr.FromInputPeer` for each dialog; that causes one resolve RPC
  per peer and is the main source of the current multi-minute runtime.
- Write fetched peer records to `peers`, then atomically replace the `dialogs`
  index and in-memory snapshot. A failed or canceled refresh must leave the
  previous index and snapshot intact.
- `dialog refresh` performs this refresh and reports only the number of cached
  dialogs after successful replacement.

## Incremental Updates

- Register dialog-cache handlers on the existing update dispatcher.
- New direct, chat, and channel messages add or update their dialog peer from
  update entities.
- User/chat/channel entity updates refresh cached names and usernames when the
  update contains enough data.
- `OnChannelInaccessible` removes inaccessible channels.
- Account-wide unrecoverable update gaps trigger a full background refresh;
  recoverable gaps continue through `getDifference`.
- Rare removals not represented by an update are corrected by the next
  `dialog refresh`.

## Names And Lookup

- Display users by visible name: trimmed `FirstName + LastName`.
- Display chats and channels by title.
- Search by visible name, username, and TDLib peer ID.
- Suggestions display the sanitized/truncated visible name already required by
  the Windows prompt renderer.
- Resolution uses the same in-memory snapshot as suggestions.
- Duplicate names return an ambiguity error containing peer IDs.

## Command Behavior

- Prompt suggestions read the in-memory dialog snapshot and perform no database
  scan or network call per keystroke.
- `dialog list` renders the current in-memory dialog snapshot immediately and
  performs no Telegram dialog-fetch RPCs.
- `dialog refresh` explicitly fetches all Telegram dialogs, atomically replaces
  the cache, and prints only a concise refreshed-dialog count.
- Remove the `cache view` command and its root `cache` command because they
  expose an internal implementation detail and duplicate `dialog list`.
- Direct commands may resolve a supplied TDLib ID without requiring dialog-cache
  membership; name-based resolution uses the dialog cache.

## Migration And Failure Handling

- Add a cache schema version. On first use of the new schema, discard old peer
  cache data and create empty `peers` and `dialogs` buckets; preserving old cache
  contents is not required.
- The first run after this reset performs one optimized bootstrap and rebuilds
  both peer data and dialog membership.
- Corrupt dialog records produce a clear cache-load error instead of silently
  mixing partial data.
- Missing or corrupt peer records referenced by the dialog index produce a clear
  cache-load error.
- Failure to persist a completed refresh is returned to the caller; replacement
  of the dialog index is performed in one bbolt transaction.

## Verification

- Store tests: schema reset, indexed load, atomic membership replacement,
  deletion of stale dialogs, rollback on failure, and all peer kinds.
- Cache tests: visible names, username aliases, TDLib IDs, duplicate names, and
  concurrent reads during replacement.
- Dialog tests: entities are converted without per-peer resolver calls.
- Prompt tests: suggestions and resolution consume the same dialog snapshot.
- Run `gofmt`, focused tests, `go test ./...`, `go build ./...`, and rebuild
  `tgdownloader.exe`.
- No commit or push before manual user review.
