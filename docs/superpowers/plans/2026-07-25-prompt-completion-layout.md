# Prompt Completion Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Bubble Tea completions fully scrollable, render peer identity robustly, and separate output, suggestions, and command input with responsive bordered panels.

**Architecture:** Completion providers return the complete ranked result set. `promptModel` owns selection and a sliding visible window, while focused rendering helpers format candidate rows and bordered panels within exact terminal dimensions. Existing Cobra execution, cache lookup, sanitization, and transcript viewport remain unchanged.

**Tech Stack:** Go 1.26.5 toolchain, Bubble Tea v2, Bubbles v2, Lip Gloss v2, Cobra, go-runewidth, uniseg.

## Global Constraints

- Render at most six suggestion rows, but retain every matching candidate.
- Preserve actual Unicode emoji and use grapheme-aware truncation.
- Keep full TDLib ID when terminal width permits; reduce name width first.
- Do not add Cyrillic `0х` normalization in this change.
- Keep every rendered line within terminal width and the complete view within terminal height.
- Do not stage, commit, or push before manual review.
- Rebuild `tgdownloader.exe` after implementation.

---

### Task 1: Complete Result Set And Sliding Selection

**Files:**
- Modify: `cmd/cmd/prompt_completion.go`
- Modify: `cmd/cmd/prompt_model.go`
- Test: `cmd/cmd/prompt_completion_test.go`
- Test: `cmd/cmd/prompt_model_test.go`

**Interfaces:**
- Consumes: `completionResult.Candidates []promptCandidate` and `promptModel.completions`.
- Produces: `promptModel.visibleCompletionCount() int`, `promptModel.visibleCompletions() []promptCandidate`, and selection/page navigation over the complete candidate set.

- [ ] **Step 1: Add failing provider tests for more than six results**

Add peer and command completion tests that create eight matching entries and assert `len(result.Candidates) == 8`. Keep the existing ranking assertions.

```go
if got := len(result.Candidates); got != 8 {
    t.Fatalf("candidate count = %d, want 8", got)
}
```

- [ ] **Step 2: Run provider tests and verify RED**

Run:

```powershell
go test ./cmd/cmd -run "TestCompletePromptRetainsAllPeerMatches|TestCompletePromptRetainsAllCommandMatches" -count=1
```

Expected: FAIL because `promptCandidates` and `promptCommandCandidates` truncate to six.

- [ ] **Step 3: Remove provider truncation**

Delete both `if len(candidates) > maxPromptPeerCompletions` truncation blocks. Rename the constant to `maxPromptVisibleCompletions` because it becomes a presentation limit only.

- [ ] **Step 4: Add failing model tests for window and page navigation**

Create eight candidates, type one character, and assert:

```go
for range 6 {
    m = updateKey(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
}
if m.selected != 6 || m.visibleCompletions()[0].Value != "two" {
    t.Fatalf("selection/window did not scroll: selected=%d visible=%v", m.selected, m.visibleCompletions())
}
```

Add `Page Down` and `Page Up` assertions that move by the current visible page size while suggestions are open. Verify arrows still control history and page keys still control transcript when suggestions are closed.

- [ ] **Step 5: Run navigation tests and verify RED**

Run:

```powershell
go test ./cmd/cmd -run "TestPromptModelScrollsCompletionWindow|TestPromptModelPagesCompletions" -count=1
```

Expected: FAIL because the model truncates from index zero and routes page keys only to the transcript.

- [ ] **Step 6: Implement full-set selection and sliding window**

Add `completionOffset int` to `promptModel`. Compute visible rows from terminal height, capped at six. Keep selected index absolute:

```go
func (m *promptModel) visibleCompletions() []promptCandidate {
    count := m.visibleCompletionCount()
    if count == 0 || len(m.completions) == 0 {
        return nil
    }
    m.ensureCompletionVisible(count)
    end := min(m.completionOffset+count, len(m.completions))
    return m.completions[m.completionOffset:end]
}
```

Arrow navigation changes `selected` across `len(m.completions)` and then calls `ensureCompletionVisible`. Page navigation changes selection by `visibleCompletionCount()`. Reset offset in `clearCompletions`. Rendering compares each visible row with `selected-completionOffset`.

- [ ] **Step 7: Run Task 1 tests**

Run:

```powershell
go test ./cmd/cmd -run "Completion|PromptModelScrolls|PromptModelPages" -count=1
```

Expected: PASS.

---

### Task 2: Peer Identity And Emoji Rendering

**Files:**
- Modify: `cmd/cmd/prompt_completion.go`
- Modify: `cmd/cmd/prompt_model.go`
- Test: `cmd/cmd/prompt_completion_test.go`
- Test: `cmd/cmd/prompt_model_test.go`

**Interfaces:**
- Consumes: `promptCandidate.Display` and `promptCandidate.Description`.
- Produces: `formatPromptCandidate(promptCandidate, int) string`, with peer descriptions in the existing `Type | 0x...` format and command descriptions as Cobra short text.

- [ ] **Step 1: Add failing emoji and full-ID row tests**

Create a cached channel named `🍒🍒🍒`, complete `download history 🍒`, and assert the candidate preserves the exact emoji string and full rendered TDLib ID. Render it at a wide width and assert both remain visible.

```go
if !strings.Contains(row, "🍒🍒🍒") || !strings.Contains(row, fullID) {
    t.Fatalf("peer identity missing from %q", row)
}
```

Add a narrow-width assertion using a family emoji sequence and verify the output contains no split UTF-8 or partial grapheme suffix.

- [ ] **Step 2: Run row-format tests and verify RED**

Run:

```powershell
go test ./cmd/cmd -run "TestPromptPeerCandidateRendersEmojiAndFullID|TestPromptPeerCandidateTruncatesNameByGrapheme" -count=1
```

Expected: FAIL because candidate descriptions are not rendered.

- [ ] **Step 3: Implement responsive candidate formatting**

Add a formatter that sanitizes fields separately. For peer descriptions, reserve width for separators, type, and full ID, then truncate only the display name with `truncatePromptText`. If width cannot hold all fields, omit type before truncating the ID. For command descriptions, render `name  short description` and truncate the description first.

```go
func formatPromptCandidate(candidate promptCandidate, width int) string {
    display := sanitizePromptModelText(candidate.Display)
    description := sanitizePromptModelText(candidate.Description)
    // Reserve description width first; truncate display by grapheme.
    return promptSingleLine(joinCandidateFields(display, description, width), width)
}
```

Use this helper for selected and unselected completion rows.

- [ ] **Step 4: Run Task 2 tests**

Run:

```powershell
go test ./cmd/cmd -run "PromptPeerCandidate|CompletePrompt.*Emoji" -count=1
```

Expected: PASS.

---

### Task 3: Responsive Bordered Panels

**Files:**
- Modify: `cmd/cmd/prompt_model.go`
- Test: `cmd/cmd/prompt_model_test.go`

**Interfaces:**
- Consumes: transcript viewport, visible completions, editor view, terminal width/height.
- Produces: `renderPromptPanel(title string, body []string, width int) []string` and exact layout allocation for `OUTPUT`, `SUGGESTIONS n/total`, and `COMMAND`.

- [ ] **Step 1: Add failing panel-boundary tests**

At widths `40`, `80`, and `120`, render a model with transcript and eight completions. Assert the output includes all three titles, `SUGGESTIONS 1/8`, and exact width/height bounds via `assertPromptLayout`.

At heights `10`, `12`, and `24`, assert command input and hint remain visible, output retains at least one row, and suggestion body shrinks below six when necessary.

- [ ] **Step 2: Run panel tests and verify RED**

Run:

```powershell
go test ./cmd/cmd -run "TestPromptModelRendersBorderedZones|TestPromptModelShrinksSuggestionsOnShortTerminal" -count=1
```

Expected: FAIL because current layout has no panel borders or titles.

- [ ] **Step 3: Implement panel renderer and height allocation**

Use a single-line Lip Gloss border with application-owned styling. Panel title replaces part of the top border. The helper returns plain line slices so total dimensions can be tested before joining.

Allocate fixed lines in this order:

1. application header: one line;
2. output panel: remaining lines, minimum three including borders;
3. suggestions panel: visible rows plus two borders;
4. command panel: editor plus two borders;
5. hint: one line.

When height is constrained, reduce suggestion rows to zero before reducing output body below one row. `visibleCompletionCount()` uses the same allocation, preventing model/render disagreement.

- [ ] **Step 4: Preserve interaction semantics inside panels**

Keep transcript viewport content unboxed internally; only its rendered lines are placed inside the output border. Keep selected completion styling inside suggestion borders. Keep the textinput prompt and cursor inside the command border without changing editor width calculations.

- [ ] **Step 5: Run Task 3 tests**

Run:

```powershell
go test ./cmd/cmd -run "PromptModel.*Layout|PromptModel.*Zone|PromptModel.*Suggestion" -count=1
```

Expected: PASS.

---

### Task 4: Regression Verification And Executable

**Files:**
- Verify all modified files.
- Rebuild: `tgdownloader.exe`.

**Interfaces:**
- Consumes: completed Tasks 1-3.
- Produces: manually reviewable `tgdownloader.exe` version `v0.6.0`.

- [ ] **Step 1: Format only touched Go files**

Run:

```powershell
gofmt -w cmd/cmd/prompt_completion.go cmd/cmd/prompt_completion_test.go cmd/cmd/prompt_model.go cmd/cmd/prompt_model_test.go
```

- [ ] **Step 2: Run full verification**

Run:

```powershell
go test ./... -count=1 -timeout 180s
go vet ./...
git diff --check
```

Expected: all commands exit `0`.

- [ ] **Step 3: Rebuild and verify executable version**

Run:

```powershell
go build -o tgdownloader.exe .
.\tgdownloader.exe version
```

Expected: `Telegram CLI Downloader v0.6.0`.

- [ ] **Step 4: Stop for manual review**

Do not stage, commit, or push. Ask the user to smoke-test:

- more than six peer and command matches;
- `Up/Down` and `PgUp/PgDn` selection;
- emoji-only channel display and insertion;
- panel layout at normal and short terminal sizes;
- ASCII `0x` ID completion, reporting exact input if it still fails.
