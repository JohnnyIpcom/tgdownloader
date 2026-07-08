# Error Model

This repository uses typed hierarchical errors via `pkg/apperr`.

## Goals

- Keep machine-readable error classification (`Kind`).
- Keep operation context (`Op`) to locate failure points quickly.
- Preserve root cause for `errors.Is` / `errors.As`.
- Return user-facing messages without hiding diagnostics.

## Core Type

- Type: `apperr.Error`
- Fields:
  - `Op`: operation name (dot-separated, stable)
  - `Kind`: category of failure
  - `Err`: wrapped cause

Use helpers:

- `apperr.New(op, kind, err)`
- `apperr.Wrap(op, err)`
- `apperr.IsKind(err, kind)`

## Error Kinds

- `KindConfig`: invalid input, unsupported mode, malformed config, invalid offset.
- `KindAuth`: auth/token/state/permission failures.
- `KindNetwork`: HTTP transport, remote status errors, timeouts.
- `KindIO`: local/stream/body read-write-close errors.
- `KindInternal`: programming invariants, request construction failures.
- `KindCancel`: context cancellation/interruption.
- `KindUnknown`: fallback when wrapping plain errors.

## Operation Naming

Use stable, explicit op names:

- Format: `<module>.<subsystem>.<action>`
- Examples:
  - `cmd.download.history.get_all_files`
  - `downloader.download`
  - `telegram.network.resolver.mtproxy`
  - `yadisk.fetch_m3u8.http`

Rules:

- Keep names lowercase and dot-separated.
- Do not include dynamic values in `Op`.
- Do not rename existing `Op` strings without migration reason.

## Patterns

### Create typed errors at boundaries

```go
return apperr.New("yadisk.fetch_m3u8.http", apperr.KindNetwork, err)
```

### Add context when crossing layers

```go
return apperr.Wrap("cmd.download.yadisk.download", err)
```

### Keep sentinel checks working

If you need `errors.Is`, keep original causes wrapped.

## Do/Don't

Do:

- Classify by failure nature, not by module.
- Wrap once per layer crossing.
- Keep `errors.Is(err, context.Canceled)` behavior intact.
- Add tests for `Kind` in important failure paths.

Don't:

- Return plain `fmt.Errorf` from critical public flows.
- Use `panic` for recoverable runtime/config errors.
- Drop original causes.

## PR Checklist

- Added/updated `Op` and `Kind` for new error paths.
- Preserved root cause (`%w` or wrapped error) for `errors.Is/As`.
- Added tests for at least one failure path `Kind` where behavior changed.
- Ran:
  - `go test ./...`
  - `go build -o tgdownloader.exe .`

## Current Status

Typed errors are applied across key runtime paths:

- `cmd/cmd` download flows
- `internal/downloader`
- `pkg/telegram` critical file/link/user/resolver paths
- `pkg/yadisk`
- `pkg/dropbox`
- `pkg/key`
- `pkg/oauth2server`
