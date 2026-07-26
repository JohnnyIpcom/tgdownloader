# Unified Bubble Tea Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Bubble Tea and Lip Gloss the only runtime terminal UI, with one ASCII progress presentation, responsive Unicode-safe tables, TUI authentication, and compact direct-command execution.

**Architecture:** Keep Cobra for parsing and domain command routing. The outer Cobra tree launches either a full-screen prompt model or an inline one-shot model before runtime initialization; both consume the same structured renderer events and execute domain commands through the existing inner Cobra tree. Telegram receives an injected authentication code provider, while progress and table producers emit structured data that the active Bubble Tea model formats at its current width.

**Tech Stack:** Go 1.25+, Cobra, Bubble Tea v2, Bubbles, Lip Gloss v2, gotd/td, afero, zap, testify.

## Global Constraints

- Preserve the existing command names, arguments, aliases, and config behavior.
- Keep `help` and `version` as ordinary non-TUI commands.
- Never read Telegram authentication codes directly from `os.Stdin` in runtime code.
- Do not add a second prompt or rendering backend.
- Do not add a progress `skip` lifecycle; existing-file skips stay in aggregate summaries.
- Sanitize Telegram-controlled text before it reaches a terminal view.
- Use Lip Gloss width measurement and grapheme-safe truncation for every aligned UI surface.
- Run `gofmt` on every changed Go file.
- Rebuild `tgdownloader.exe` after every checkpoint presented for manual review.
- Do not commit a checkpoint until the user has manually reviewed and approved it.
- After approval, commit only that checkpoint so every subsequent review has its own commit.

## Execution Status

- Task 1 complete: shared ASCII progress shipped through `e986e01` and follow-up fixes.
- Task 2 complete: `caa0399 refactor(renderer): remove go-pretty`.
- Task 3 complete: `1abefd5 feat(prompt): move startup into TUI`.
- Task 4 complete: `7f1a04a feat(prompt): render one-shot commands in TUI`.
- Task 5 automated verification complete. Final manual review is pending; fresh-code authentication remains manually unverified.

---

## Task 1: Structured Progress Events And Shared ASCII Formatter

**Files:**
- Modify: `internal/renderer/events.go`
- Modify: `internal/renderer/events_test.go`
- Modify: `internal/renderer/tui_progress.go`
- Modify: `internal/renderer/tui_progress_test.go`
- Create: `internal/renderer/progress_format.go`
- Create: `internal/renderer/progress_format_test.go`
- Modify: `cmd/cmd/prompt_model.go`
- Modify: `cmd/cmd/prompt_model_test.go`
- Modify: `cmd/cmd/download_progress_test.go`

### Event contract

- [ ] Add failing tests for typed progress events. Keep `Event.Text` for ordinary output lines, but make progress events carry raw fields:

```go
type ProgressUnit uint8

const (
	ProgressUnitCount ProgressUnit = iota
	ProgressUnitBytes
)

type Event struct {
	Kind    EventKind
	ID      string
	Text    string
	Label   string
	Current int64
	Total   int64
	Unit    ProgressUnit
	Elapsed time.Duration
}
```

- [ ] Test that create, update, done, and fail events retain the same stable ID and raw label.
- [ ] Test that elapsed time starts at tracker creation and is populated on terminal events.
- [ ] Test that byte trackers emit `ProgressUnitBytes`, while count trackers emit `ProgressUnitCount`.
- [ ] Run `go test ./internal/renderer -run 'Test(Event|TUIProgress)'` and confirm the new tests fail for missing fields or behavior.
- [ ] Change the event-backed tracker to store `label`, `unit`, and `startedAt`, and emit structured fields instead of preformatted status text.
- [ ] Run the focused tests and confirm they pass.

### ASCII formatter

- [ ] Add table-driven failing tests for `FormatProgress(event Event, width int, frame int) string` covering:
  - known count totals;
  - known byte totals;
  - unknown totals with a moving `#` indicator;
  - `done!` and `fail!` after the bar;
  - terminal elapsed time;
  - zero totals and values above total;
  - widths from 30 through 160 columns;
  - Cyrillic, combining marks, ZWJ emoji, and regional-indicator flags in labels.
- [ ] Assert that the visible width never exceeds the supplied width and that aligned rows place `[` in the same column when space permits.
- [ ] Run `go test ./internal/renderer -run TestFormatProgress` and confirm failure.
- [ ] Implement one formatter with these degradation rules, in order: hide current/total values, reduce the label column, reduce the bar to its minimum, then truncate the whole row as a last resort.
- [ ] Use only `[`, `]`, `#`, and `.` for the bar. Apply Lip Gloss styles after layout calculations so ANSI sequences never affect width.
- [ ] Render active progress neutrally, `done!` green, and `fail!` red. Do not emit a skip status.
- [ ] Run `go test ./internal/renderer -run TestFormatProgress` and confirm all cases pass.

### Prompt integration

- [ ] Replace `bubbles/progress` rendering in `promptModel.renderProgressRow` with `renderer.FormatProgress`.
- [ ] Remove model fields and commands used only by `bubbles/progress`; keep the existing animation tick only for unknown-total movement.
- [ ] Update prompt model tests to assert status position, stable alignment, narrow rendering, and Unicode labels.
- [ ] Run `go test ./cmd/cmd -run 'TestPrompt.*Progress|TestDownloadProgress'`.
- [ ] Run `gofmt` on changed files, then `go test ./internal/renderer ./cmd/cmd`.
- [ ] Rebuild with `go build -o tgdownloader.exe .`.
- [ ] Present checkpoint 1 for manual review. Do not stage or commit yet.

**After explicit approval only:**

- [ ] Stage only Task 1 files.
- [ ] Commit with `feat(renderer): unify ASCII progress rows`.

---

## Task 2: Structured Responsive Tables And Complete go-pretty Removal

**Files:**
- Modify: `internal/renderer/events.go`
- Modify: `internal/renderer/events_test.go`
- Create: `internal/renderer/table.go`
- Create: `internal/renderer/table_test.go`
- Modify: `internal/renderer/dialog.go`
- Modify: `internal/renderer/dialog_test.go`
- Modify: `internal/renderer/peer.go`
- Modify: `internal/renderer/user.go`
- Modify: `internal/renderer/utils.go`
- Modify: `internal/renderer/utils_test.go`
- Modify: `internal/renderer/simple.go`
- Modify: `cmd/cmd/prompt_model.go`
- Modify: `cmd/cmd/prompt_model_test.go`
- Modify call sites under: `cmd/cmd`
- Delete: `internal/renderer/progress.go`
- Delete or replace: `internal/renderer/progress_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

### Structured table model

- [ ] Add failing tests for a structured table event:

```go
type TableAlign uint8

const (
	TableAlignLeft TableAlign = iota
	TableAlignRight
)

type TableColumn struct {
	Header       string
	MinWidth     int
	Priority     int
	Align        TableAlign
	Required     bool
}

type TableData struct {
	Columns []TableColumn
	Rows    [][]string
}
```

- [ ] Add `EventTable` and `Event.Table *TableData`; reject malformed tables in tests instead of panicking.
- [ ] Add failing formatter tests for `FormatTable(table TableData, width int) []string` covering dialogs, peers, and users at 50, 80, 120, and 180 columns.
- [ ] Assert right-aligned IDs, grapheme-safe name truncation, column hiding by priority, and no row wider than the terminal.
- [ ] Include exact regressions for flags, skin-tone modifiers, ZWJ emoji, combining marks, and names longer than 100 characters.
- [ ] Run `go test ./internal/renderer -run 'Test(Table|FormatTable)'` and confirm failure.
- [ ] Implement layout in two passes: reserve required columns first, then add optional columns in priority order; distribute remaining width to flexible text columns.
- [ ] Measure and truncate with Lip Gloss-compatible grapheme handling. Sanitize cell text before measurement.
- [ ] Run the focused table tests.

### Renderer migration

- [ ] Change dialog, peer, and user renderers to create `TableData` and emit `EventTable` through the context event sink.
- [ ] Keep a deterministic writer fallback for unit tests and non-runtime library callers by formatting at the writer's configured/default width; runtime command paths must use structured events.
- [ ] Remove HTML entity emoji replacement. Preserve valid display emoji and strip only unsafe terminal controls through the existing sanitization path.
- [ ] Replace `go-pretty/text` styles in `simple.go` and helpers with Lip Gloss styles.
- [ ] Add renderer tests proving the same structured rows are used by dialog, peer, and user output.

### Responsive prompt output

- [ ] Replace the prompt transcript's flat string-only storage with output blocks that retain either sanitized text or `TableData`.
- [ ] Reformat table blocks in `syncViewportContent` using the current viewport width, so terminal resize reflows existing tables.
- [ ] Add a model test that sends a width-change message and verifies that a previously emitted table becomes narrower without losing required IDs.
- [ ] Run `go test ./cmd/cmd -run 'TestPrompt.*(Table|Resize|Output)'`.

### Dependency removal

- [ ] Replace `renderer.Progress` construction and adapters with the event-backed progress implementation from Task 1.
- [ ] Delete the legacy `go-pretty/progress` implementation and migrate its remaining tests to structured progress tests.
- [ ] Run `rg -n 'go-pretty|jedib0t' --glob '*.go' --glob 'go.mod' --glob 'go.sum'` and require zero results after `go mod tidy`.
- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go mod tidy`.
- [ ] Run `go test ./internal/renderer ./cmd/cmd ./pkg/telegram ./internal/downloader`.
- [ ] Rebuild with `go build -o tgdownloader.exe .`.
- [ ] Present checkpoint 2 for manual review, including dialog list, peer output, long names, and emoji. Do not stage or commit yet.

**After explicit approval only:**

- [ ] Stage only Task 2 files.
- [ ] Commit with `refactor(renderer): remove go-pretty`.

---

## Task 3: Start TUI Before Runtime And Handle Authentication In COMMAND

**Files:**
- Create: `pkg/telegram/auth_code.go`
- Create: `pkg/telegram/auth_code_test.go`
- Modify: `pkg/telegram/client.go`
- Modify: `pkg/telegram/client_options_test.go`
- Create: `cmd/cmd/prompt_auth.go`
- Create: `cmd/cmd/prompt_auth_test.go`
- Create: `cmd/cmd/runtime_startup.go`
- Create: `cmd/cmd/runtime_startup_test.go`
- Modify: `cmd/cmd/root.go`
- Modify: `cmd/cmd/root_test.go`
- Modify: `cmd/cmd/prompt.go`
- Modify: `cmd/cmd/prompt_model.go`
- Modify: `cmd/cmd/prompt_model_test.go`
- Modify: `cmd/cmd/prompt_tui.go`
- Modify: `cmd/cmd/prompt_tui_test.go`

### Injected code provider

- [ ] Add failing Telegram tests for:

```go
type CodeProvider interface {
	Code(context.Context, *tg.AuthSentCode) (string, error)
}
```

- [ ] Add a client option `WithCodeProvider(CodeProvider)` and require `Auth` to use it instead of constructing a stdin reader.
- [ ] Test successful code delivery, provider error propagation, and cancellation while waiting.
- [ ] Run `go test ./pkg/telegram -run 'Test.*CodeProvider|Test.*Auth'` and confirm failure.
- [ ] Implement the option and remove `fmt.Print`, `bufio.Reader`, and direct `os.Stdin` code entry from `pkg/telegram/client.go`.
- [ ] Run the focused Telegram tests.

### Channel-backed TUI provider

- [ ] Add failing tests for a channel-backed provider that sends a request message to the model and waits for exactly one reply or context cancellation.
- [ ] Define request and response types that carry no logging or history metadata.
- [ ] Implement cancellation so neither the authenticator goroutine nor the Bubble Tea model can remain blocked.
- [ ] Run `go test ./cmd/cmd -run TestTUIAuthCodeProvider`.

### Startup state machine

- [ ] Add model states for starting, awaiting authentication code, ready, failed, and stopping.
- [ ] Add failing transition tests for startup success, code request/submission, provider cancellation, startup failure, and Ctrl+C from every non-ready state.
- [ ] In code mode, render COMMAND as `code>`, mask entered runes, disable completions/history, and clear the buffer immediately after submission.
- [ ] Prove in tests that submitted codes never enter output events, prompt history, error strings, or debug logging arguments.
- [ ] Create a single startup `tea.Cmd` that initializes config/logging/client, attaches the event sink and code provider, connects, authenticates, starts cache/update work, and obtains prompt user/history data.
- [ ] Move `Connect` and prompt setup out of `newPromptCmd.RunE`; `RunE` must start Bubble Tea first and let the startup command do runtime work.
- [ ] Ensure completion and ordinary command submission remain disabled until startup reaches ready.
- [ ] On startup error, keep the failed row and sanitized error visible, keep COMMAND disabled, wait for Ctrl+C, then return the original error.
- [ ] Join startup work and close runtime resources before `tea.Program.Run` returns.
- [ ] Run `go test ./cmd/cmd -run 'Test(RuntimeStartup|Prompt.*Startup|Prompt.*Auth)'`.
- [ ] Run `gofmt` on changed files, then `go test ./pkg/telegram ./cmd/cmd`.
- [ ] Rebuild with `go build -o tgdownloader.exe .`.
- [ ] Present checkpoint 3 for manual review: normal startup, code authentication if available, and a controlled startup failure. Do not stage or commit yet.

**After explicit approval only:**

- [ ] Stage only Task 3 files.
- [ ] Commit with `feat(tui): move startup and auth into Bubble Tea`.

---

## Task 4: Compact Bubble Tea Mode For Direct Runtime Commands

**Files:**
- Create: `cmd/cmd/oneshot_model.go`
- Create: `cmd/cmd/oneshot_model_test.go`
- Create: `cmd/cmd/runtime_command.go`
- Create: `cmd/cmd/runtime_command_test.go`
- Modify: `cmd/cmd/root.go`
- Modify: `cmd/cmd/root_test.go`
- Modify: `cmd/cmd/prompt_execution.go`
- Modify: `cmd/cmd/prompt_execution_test.go`
- Modify: `cmd/cmd/dialogs.go`
- Modify: `cmd/cmd/download.go`
- Modify: `cmd/cmd/peers.go`

### Separate routing from execution

- [ ] Add failing root tests proving `help` and `version` never launch a Bubble Tea runner, while commands annotated `runtime_only` or `requires_connection` launch it exactly once.
- [ ] Keep the inner command tree returned by `newPromptRootCmd` as the only place domain `RunE` handlers execute.
- [ ] Make the outer runtime commands route their resolved path, flags, and arguments into a one-shot request instead of invoking `initializeRuntime` or `Connect` in Cobra hooks.
- [ ] Remove runtime initialization and connection side effects from outer `PreRunE`/`PostRunE`. Preserve the annotations as routing metadata.
- [ ] Ensure persistent flags, command-local flags, aliases, validation errors, and exit codes match direct Cobra execution.
- [ ] Run `go test ./cmd/cmd -run 'TestRoot.*(Runtime|Help|Version)|TestRuntimeCommandRouting'`.

### One-shot model

- [ ] Add failing model tests for startup, command execution, progress replacement by stable tracker ID, table output, completion, failure, and Ctrl+C.
- [ ] Implement a compact model with `AltScreen: false`; it consumes the same event types and calls the same progress/table formatters as prompt mode.
- [ ] Share startup/auth code handling with Task 3. During authentication, render a masked inline `code>` input.
- [ ] Execute the selected command through the inner Cobra tree with a context containing the event sink. Do not recursively launch another TUI.
- [ ] Keep final progress rows, summaries, table output, and sanitized errors in scrollback after the program exits.
- [ ] Return the original startup or command error to `Root.Execute` after rendering has stopped.
- [ ] Ensure active startup/command work is cancelled and joined before renderer/client shutdown.
- [ ] Run `go test ./cmd/cmd -run 'TestOneShot|TestRuntimeCommand'`.
- [ ] Run `gofmt` on changed files, then `go test ./cmd/cmd ./internal/renderer ./pkg/telegram ./internal/downloader`.
- [ ] Rebuild with `go build -o tgdownloader.exe .`.
- [ ] Present checkpoint 4 for manual review using direct `dialog list`, `dialog refresh`, peer lookup, and a short download. Do not stage or commit yet.

**After explicit approval only:**

- [ ] Stage only Task 4 files.
- [ ] Commit with `feat(tui): render direct commands with Bubble Tea`.

---

## Task 5: Hardening, Version Bump, And End-To-End Verification

**Files:**
- Modify: tests discovered by failures under `cmd/cmd`, `internal/renderer`, `internal/downloader`, and `pkg/telegram`
- Modify: the existing version declaration identified by `rg -n '0\.6\.0|version' cmd main.go`
- Modify: `README.md` only if it documents obsolete prompt controls or direct-command output
- Modify: `docs/superpowers/plans/2026-07-25-unified-bubble-tea-rendering.md` checkbox states during execution

### Automated hardening

- [x] Add regression tests for late events after model shutdown, cancellation during auth, cancellation during download, duplicate tracker IDs, malformed tables, and terminal widths below the normal minimum.
- [x] Add an integration-level test that executes one representative runtime command in prompt mode and one-shot mode against fakes and compares their final progress/table content after stripping mode-specific framing.
- [x] Run `go test -race ./internal/renderer ./cmd/cmd ./pkg/telegram ./internal/downloader` where supported; if Windows dependencies prevent race execution, record the exact limitation and run the same packages without `-race`.
- [x] Run `go test ./...`.
- [x] Run `go vet ./...`.
- [x] Run `rg -n 'go-pretty|jedib0t|bufio.NewReader\(os.Stdin\)|bubbles/progress' --glob '*.go' --glob 'go.mod' --glob 'go.sum'` and require zero obsolete runtime-rendering matches.
- [x] Run `gofmt` across all changed Go files and confirm `git diff --check` is clean.

### Version and executable

- [x] Raise the application version from `v0.6.0` to `v0.7.0` because the runtime UI and direct-command lifecycle change materially.
- [x] Build `tgdownloader.exe` with `go build -o tgdownloader.exe .`.
- [x] Run `./tgdownloader.exe version` and verify it prints `v0.7.0` without entering Bubble Tea.

### Manual smoke matrix

- [x] Prompt startup with an existing session: every stage appears immediately and ends with the shared ASCII status format.
- [ ] Prompt startup requiring a Telegram code: COMMAND changes to masked `code>`, submission resumes startup, and the code is absent from history/logs/output.
- [x] Startup with deliberately invalid connection configuration: failed row and error remain visible until Ctrl+C; no command input or retry appears.
- [x] Prompt `dialog list`: table remains aligned at wide and narrow widths and after resizing.
- [x] Prompt completion: command, name substring, hexadecimal ID, multiword name, and emoji-only channel selection work.
- [x] Prompt media download: labels and ASCII bars align; final `done!`/`fail!` follows the bar; duplicate names display their actual resolved filenames.
- [x] Direct `dialog list`, `dialog refresh`, peer lookup, and media download use compact inline Bubble Tea and leave readable final output in scrollback.
- [x] Ctrl+C during startup and download exits without a panic, blocked goroutine, stale alternate screen, or unclosed client.
- [x] Recheck long names, Cyrillic, combining marks, ZWJ emoji, flags, and emoji-only names in tables and progress labels.
- [x] Present checkpoint 5 for final manual review. Do not stage, commit, merge, or push yet.

**After explicit approval only:**

- [x] Stage the Task 5 files and any approved plan/spec documents.
- [x] Commit with `feat(tui): complete unified runtime interface`.
- [x] Push only when the user explicitly says to publish.

---

## Completion Criteria

- [x] Bubble Tea starts before any runtime initialization for prompt and direct runtime commands.
- [x] Startup, authentication, cache/update work, downloads, and direct commands share one structured event path.
- [x] Every progress row uses the ASCII formatter and places terminal status after the bar.
- [x] Dialog, peer, and user tables stay within terminal width and preserve Unicode graphemes.
- [x] Telegram authentication code entry is masked, cancellable, and absent from logs/history.
- [x] `go-pretty` and `bubbles/progress` are absent from runtime code and module dependencies.
- [x] `help` and `version` remain plain Cobra output.
- [x] Full tests, vet, formatting, executable build, and the reviewed manual smoke cases pass.
- [x] No checkpoint was committed before user review.
