# Bubble Tea Prompt UX Redesign

## Goal

Replace both `c-bata/go-prompt` and the rejected `reeflective/console`
prototype with one Bubble Tea prompt implementation. Preserve the existing
non-interactive Cobra CLI while making interactive input, peer completion,
progress, errors, and Unicode rendering stable in Windows Terminal.

This is an experiment on the existing feature branch. There is no fallback
prompt engine and no `prompt.engine` configuration setting.

## Screen Layout

Prompt mode uses the terminal alternate screen and has four stable regions:

1. A one-line header showing the authenticated user, connection state, and
   application version.
2. A scrollable transcript containing submitted commands, command output,
   progress updates, errors, and summaries.
3. A completion area directly above the editor, showing at most six rows.
4. A bottom editor and context-sensitive key hints.

The editor always remains at the bottom. Completion rows are single-column and
use bounded display names. Resizing recomputes available transcript and
completion dimensions without changing command state.

## Interaction

The prompt accepts the same command lines as the non-interactive CLI. Peer
completion searches visible names and aliases by case-insensitive substring.
TDLib ID input keeps case-insensitive prefix completion. Results come from the
dialog cache and are ranked by exact match, prefix match, substring match, then
visible name.

Up and Down select completion rows while the completion list is open. Enter
accepts a selected completion; otherwise it submits a complete command. Escape
closes completions. With completions closed, Up and Down navigate history.

Only one command may execute at a time. Submission closes completions and
blocks the editor until the command finishes. `Ctrl+C` cancels the active
command. At idle, `Ctrl+C`, `Ctrl+D`, or the `exit` command closes prompt mode.

## Architecture

One Bubble Tea model owns terminal input and rendering. Bubbles `textinput` is
used only for editing. Completion matching, selection, display width, and
insertion are application code so no third-party matcher can discard substring
results or rearrange columns.

Cobra remains the source of command definitions, argument validation, flags,
help, and execution. Keeping Cobra avoids a second parser and keeps prompt and
one-shot commands behaviorally identical. The existing splitter converts an
accepted line to Cobra arguments, including quoted multi-word peer names and
Windows paths.

An accepted command runs asynchronously with a dedicated cancellable context.
The model receives command lifecycle and renderer events through Bubble Tea
messages. Command completion restores editor focus and permits the next
submission.

## Renderer Bridge

Bubble Tea must be the only terminal writer while prompt mode is active.
Prompt execution therefore uses a renderer adapter that converts these events
to model messages:

- transcript lines and tables;
- tracker creation and progress updates;
- tracker completion or failure;
- download plan and final summary;
- concise user errors and cancellation.

The adapter is scoped to the prompt command context and does not intercept or
replace process-global stdout. Non-interactive commands continue using the
current go-pretty renderers. Existing renderer formatting logic should be
shared where practical, but terminal writing and TUI event delivery remain
separate implementations.

Progress rows have stable identities. Updates replace the matching active row
instead of appending a new transcript line. Once completed, failed, or
cancelled, a progress row becomes an immutable transcript entry. This preserves
scan status and file outcomes without screen drift.

## History And Completion

The existing history file, maximum-entry limit, and sensitive-argument filter
remain. Accepted valid commands are stored; authentication secrets are not.

Peer completion reads only the fast dialog cache. It does not perform a dialog
refresh or network request while typing. Cache errors appear as a non-selectable
status row and never close the TUI. Full insertion values are independent from
bounded display values, so long names remain executable.

## Errors And Recovery

Expected command errors are appended to the transcript in concise form and
return focus to the editor. Debug verbosity may add internal operation and kind
details. Cancellation is reported as `Interrupted`, not as a failure.

A panic in asynchronous command execution is recovered at the command boundary,
logged to the transcript, and returns the model to idle state. A panic must not
leave the editor permanently blocked.

## Dependency Cleanup

Remove `c-bata/go-prompt`, `reeflective/console`, direct readline integration,
Carapace completion code, both old prompt implementations, and
`prompt.engine`. Add Bubble Tea and the minimal Bubbles modules used by the
model. There is one prompt implementation.

## Verification

Unit tests cover line parsing, quoted names, Windows paths, substring and ID
completion, ranking, Unicode sanitization, long display names, full insertion
values, and history filtering.

Model tests drive Bubble Tea key and resize messages to verify completion
navigation, history navigation, submission, blocked input, cancellation,
recovery, and viewport dimensions. Renderer adapter tests verify concurrent
progress updates, stable row identity, completion, failure, and transcript
promotion. An integration test runs a fake asynchronous Cobra command and
checks editor state across its lifecycle.

Manual review uses Windows Terminal at 80x25, 120x30, and a narrow resized
window. It covers Cyrillic, emoji, names over 100 characters, substring and ID
search, selection and insertion, history, command errors, download progress,
`Ctrl+C`, and return to idle prompt.

After verification, run `gofmt`, `go mod tidy`, the full test suite, and rebuild
`tgdownloader.exe`. Do not commit or push before manual review.
