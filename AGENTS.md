# AGENTS

## Scope

These instructions apply to the whole repository.

## Project Overview

- `tgdownloader` is a Go CLI for browsing Telegram peers/dialogs and downloading files.
- Entry point: [main.go](main.go)
- CLI setup and lifecycle live in [cmd/cmd/root.go](cmd/cmd/root.go)
- Telegram client and service layer live in [pkg/telegram](pkg/telegram)
- Download orchestration lives in [internal/downloader](internal/downloader)
- Terminal rendering lives in [internal/renderer](internal/renderer)

## First Files To Read

- [cmd/cmd/root.go](cmd/cmd/root.go) for command registration, config loading, logger setup, and connect/disconnect behavior.
- [cmd/cmd/download.go](cmd/cmd/download.go) for the main download workflow.
- [pkg/telegram/client.go](pkg/telegram/client.go) for Telegram client initialization and shared services.
- [internal/downloader/downloader.go](internal/downloader/downloader.go) for worker-pool behavior and file write flow.
- [tgdownloader.yaml.template](tgdownloader.yaml.template) for the expected config shape.

## Build And Validation

- Go version: `go 1.25.0` in [go.mod](go.mod).
- Preferred build check: `go build ./...`
- There are focused `*_test.go` files in [pkg/telegram](pkg/telegram).
- After changes to CLI or Telegram flows, prefer a narrow manual CLI check over broad refactors.

## Runtime And Config Rules

- Config is required very early in startup. `NewRoot()` loads config via Viper before command execution.
- Default config lookup is `~/tgdownloader.*` and `./tgdownloader.*` with config name `tgdownloader`; see [pkg/config/viper/config.go](pkg/config/viper/config.go).
- Environment overrides use the `TGDOWNLOADER_` prefix with `.` replaced by `_`.
- Use [tgdownloader.yaml.template](tgdownloader.yaml.template) as the source of truth for config keys.
- Session state is stored in [session.json](session.json); cache is typically stored in `storage.db` per the config template.
- Telegram public keys are expected at the repo root: `public_key_test.pem` and `public_key_prod.pem`.

## Architecture Notes

- The CLI layer in [cmd/cmd](cmd/cmd) wires Cobra commands to the Telegram services and downloader.
- `Root.setupConnectionForCmd` is the key lifecycle hook: commands connect in `PreRunE` and may disconnect in `PostRunE`.
- The Telegram package exposes service-style responsibilities such as file, peer, dialog, cache, and user operations.
- The downloader uses option functions and defaults to `runtime.NumCPU()` workers; see [internal/downloader/downloader.go](internal/downloader/downloader.go).
- File and storage code commonly use `afero.Fs`; keep that abstraction when changing filesystem behavior.

## gotd Workflow

- Keep the lifecycle order aligned with gotd examples: `client.Run` -> `Auth().IfNecessary` -> `updates.Manager.Run`.
- Derive `updates.AuthOptions.IsBot` from the authenticated user (`user.GetBot()`), not from hardcoded values.
- Avoid forcing `updates.AuthOptions.Forget` unless there is an explicit reason to reset local update state.
- Ensure storage resources are closed by the caller on shutdown (`Client.Close()` is expected to run from root lifecycle paths).

## Conventions For Changes

- Keep edits localized to the owning layer: command wiring in `cmd/cmd`, Telegram API logic in `pkg/telegram`, download execution in `internal/downloader`.
- Preserve existing option-function patterns instead of introducing new config structs for small feature additions.
- Keep structured logging through `zap`/`logr`; do not introduce ad hoc printing in library code.
- When changing config-driven behavior, update [tgdownloader.yaml.template](tgdownloader.yaml.template) in the same change.
- When changing command behavior, review whether connection setup should still happen through `setupConnectionForCmd` rather than inside the command body.

## Common Pitfalls

- Missing config is a startup failure, not a lazy runtime fallback.
- Changes that bypass `Root.Close()` can leave the Telegram client connected or skip renderer shutdown.
- The prompt command is intentionally added last in [cmd/cmd/root.go](cmd/cmd/root.go); preserve that ordering unless there is a strong reason to change it.
- Manual validation may require a valid Telegram app id/hash, phone number, and existing session state.
