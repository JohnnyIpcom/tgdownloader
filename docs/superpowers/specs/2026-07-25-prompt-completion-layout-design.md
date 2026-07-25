# Prompt Completion Layout Design

## Scope

Improve Bubble Tea prompt completion browsing and visual separation after smoke testing.
This change does not alter command execution, dialog cache contents, or TDLib ID parsing.
Support for a Cyrillic `х` in an ID prefix is explicitly deferred until the input issue is reproduced.

## Layout

The prompt uses three bordered panels:

1. `OUTPUT` contains command history, command output, and completed progress rows.
2. `SUGGESTIONS current/total` contains completion candidates.
3. `COMMAND` contains the editable command line.

The existing application header remains above the panels. The contextual key hint remains below the
command panel. Borders use restrained terminal colors and must not cause any rendered line to exceed
the terminal width.

The output panel receives all remaining vertical space. The suggestions panel shows at most six
candidates. On short terminals it reduces its visible row count so the header, command editor, hint,
and at least one output row remain visible.

## Completion Set And Navigation

Completion providers retain every matching candidate instead of truncating the result to six items.
The model renders a sliding window over that complete result set.

- `Up` and `Down` move selection by one candidate and scroll the window when needed.
- `Page Up` and `Page Down` move selection by one visible page while suggestions are open.
- Selection wraps at the beginning and end, matching current arrow behavior.
- `Enter` inserts the selected candidate.
- `Escape` closes suggestions.
- The panel title displays the selected one-based position and total, for example
  `SUGGESTIONS 2/147`.

When suggestions are closed, existing transcript paging and command-history navigation retain their
current key behavior.

## Candidate Rendering

Peer candidates render three fields:

`visible name | peer type | full TDLib peer ID`

The visible name is the primary field. Emoji are preserved as Unicode and participate in substring
matching, so an emoji-only channel can be found by pasting its emoji. The full TDLib ID remains visible
when width permits and provides a reliable identifier if the terminal font renders an emoji poorly.

Responsive truncation removes space from the name first. Type and ID are retained before the name is
reduced further. On terminals too narrow for all fields, the ID is retained and the type may be hidden.
Truncation remains grapheme-aware so emoji sequences are not split.

Command candidates use the same scrolling model but render command name and Cobra short description;
they do not render peer type or ID.

## ID Completion

Existing ASCII `0x` TDLib ID prefix matching remains unchanged in this change. A separate fix will be
made only if another smoke test reproduces failure with an ASCII prefix. Cyrillic `0х` normalization is
not included.

## Safety And Error Handling

Terminal control-sequence sanitization remains active. Candidate text and panel titles are sanitized
before styling. Borders and styles are application-owned and applied after sanitization.

Completion-cache errors occupy the suggestions panel status row and do not enter the transcript.
An empty result renders an empty suggestions panel only while completion context is active; it does not
block command submission.

## Tests

Add focused model and completion tests for:

- retaining more than six candidates;
- scrolling the six-row window with arrows;
- page navigation and selection position;
- responsive reduction of visible rows;
- panel separation and strict width/height bounds;
- peer rows containing actual emoji, peer type, and full ID;
- grapheme-safe name truncation;
- command candidate scrolling;
- unchanged ASCII `0x` prefix matching.

Run `gofmt` only on touched Go files, then `go test ./...`, `go vet ./...`, `git diff --check`, and
rebuild `tgdownloader.exe`. Do not stage, commit, or push before manual review.
