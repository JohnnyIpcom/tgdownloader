# Unified Bubble Tea Rendering Design

## Goal

Use Bubble Tea and Lip Gloss as the only terminal UI stack for runtime commands. Remove `go-pretty` completely, unify startup and media progress rendering, and move Telegram authentication code entry into the TUI.

## Scope

- Start Bubble Tea before runtime initialization for every runtime command.
- Support full-screen interactive prompt and compact one-shot command modes.
- Render startup, command, and media progress through one structured event pipeline.
- Replace all `go-pretty/progress`, `go-pretty/table`, and `go-pretty/text` usage.
- Preserve Cobra as the command parser and command router.
- Preserve plain non-runtime behavior for `help` and `version`.

## Runtime Architecture

The outer Cobra tree parses arguments but does not connect to Telegram or write runtime UI directly. Runtime commands launch a Bubble Tea program and pass the selected command path and arguments to its model.

Runtime initialization runs in a background `tea.Cmd`:

1. Load configuration and initialize logging.
2. Construct the Telegram client and dialog cache.
3. Attach the structured renderer event sink.
4. Connect to Telegram.
5. Authenticate when necessary.
6. Start dialog cache bootstrap and update tracking.
7. Load prompt history and the authenticated user when prompt mode needs them.
8. Enter prompt-ready state or execute the selected one-shot command.

The model owns cancellation and waits for active initialization or command work before returning. Cobra command implementations continue to own domain behavior; they receive a context carrying the renderer event sink.

## TUI Modes

### Prompt Mode

Prompt mode uses the alternate screen and the existing OUTPUT, SUGGESTIONS, and COMMAND panels. Startup progress appears in OUTPUT before COMMAND becomes available. Completion is disabled until initialization succeeds.

### One-Shot Mode

Direct runtime commands use a compact inline Bubble Tea view. The final progress rows, output, summary, or error remain in terminal scrollback when the command exits.

`help` and `version` bypass Bubble Tea and remain ordinary CLI output.

## Startup Failure

If configuration, client creation, connection, authentication, cache bootstrap, or update startup fails:

- The failed progress row ends with `fail!`.
- OUTPUT displays the sanitized error.
- Command entry remains disabled.
- The TUI stays open until `Ctrl+C`.
- The process returns the original error after the TUI closes.

There is no retry action in this change.

## Authentication

Replace the hard-coded stdin code reader with an injected code-provider interface.

- Ordinary non-TUI tests may use a direct provider.
- Runtime TUI modes use a channel-backed provider.
- A Telegram code request switches COMMAND into `code>` mode.
- Input is masked and is never stored in prompt history or logs.
- Submitting sends the code to the blocked authenticator and returns the model to startup mode.
- Cancellation releases both the model and authenticator goroutine.

Phone number and 2FA password remain configuration-driven. Interactive phone and password forms are outside this change.

## Structured Progress Events

Progress producers emit data rather than formatted terminal strings. A progress event carries:

- Stable tracker ID.
- Label.
- Current and total values.
- Unit kind: count or bytes.
- Start time and elapsed time.
- Lifecycle kind: create, update, done, or fail.

There is no skip lifecycle state. Existing-file skips remain represented by aggregate scan and download summaries.

## Progress Presentation

Known totals use one ASCII style:

```text
Connecting             [######..............]  30.0%
Authentication         [####################] 100.0%  done! [55ms]
video.mp4               [########............]  40.0%  3.67/9.18 MB
AnimatedSticker.tgs     [....................]   0.0%  fail! [0 B in 202ms]
```

Unknown totals use a moving ASCII indicator inside the same brackets. The formatter follows these rules:

- Characters are `[`, `#`, and `.` only.
- Labels occupy a responsive fixed column and truncate by grapheme.
- Bars occupy one aligned column whenever terminal width permits.
- Percentage and values follow the bar.
- `done!` and `fail!` appear after the bar.
- Terminal states include elapsed time.
- Done is green, fail is red, and active progress uses a restrained neutral color.
- Width and truncation use Lip Gloss consistently.
- Narrow layouts remove optional values before shrinking the bar or label.

Startup, prompt commands, media downloads, Yandex downloads, and direct commands use this formatter.

## Table Rendering

Replace `go-pretty/table` with a Lip Gloss-aware table formatter used by dialog, peer, and user output.

- Column definitions declare alignment, minimum width, and truncation priority.
- Width calculations use Lip Gloss.
- Long names truncate by grapheme and preserve emoji sequences.
- Low-priority columns disappear before required identifiers.
- Rows never exceed the available terminal width.
- Plain one-shot output and prompt OUTPUT use the same formatted rows.

## Text Rendering

Replace `go-pretty/text` color and width helpers with Lip Gloss styles and the existing sanitization layer. No producer may insert unsanitized Telegram text into a rendered view.

## Dependency Removal

After progress, table, and text migration:

- Delete the legacy live progress implementation.
- Remove all `github.com/jedib0t/go-pretty/v6` imports.
- Run `go mod tidy` and verify the module disappears from `go.mod` and `go.sum`.

## Error Handling And Cancellation

- Every background command observes the model lifetime context.
- Event producers stop when the sink lifetime ends.
- Authentication requests unblock on cancellation.
- Active command and initialization goroutines are joined before program exit.
- Late progress events cannot overwrite terminal rows.
- Errors remain sanitized before entering OUTPUT.

## Verification

Automated coverage includes:

- ASCII progress formatting at wide and narrow widths.
- Known and unknown totals.
- Done and fail status placement after the bar.
- Byte values and elapsed time.
- Unicode labels, combining marks, ZWJ emoji, and regional-indicator flags.
- Authentication request, submit, cancellation, and history exclusion.
- Startup success and failure state transitions.
- Prompt and one-shot runtime execution.
- Responsive dialog, peer, and user tables.
- A repository check that no `go-pretty` import remains.

Manual review checkpoints are:

1. Shared ASCII progress rows and structured events.
2. Lip Gloss table migration and `go-pretty` removal.
3. Early Bubble Tea startup and authentication code mode.
4. One-shot runtime command mode.
5. End-to-end startup, media download, failure, and narrow-terminal smoke tests.

Each checkpoint is committed only after user review. `tgdownloader.exe` is rebuilt after every accepted fix or checkpoint.
