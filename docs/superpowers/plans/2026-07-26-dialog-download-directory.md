# Dialog Download Directory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store every Telegram history download beneath the selected dialog's directory instead of message-author directories.

**Architecture:** The command layer owns dialog directory selection because it has the requested peer. It passes that directory explicitly into `downloader.NewFile`; the downloader stops interpreting author metadata as a path instruction.

**Tech Stack:** Go 1.25+, Cobra command layer, afero downloader tests.

## Global Constraints

- Output layout is `downloads/<dialog name>/<file>`.
- Invalid Windows component characters and control characters are removed.
- Empty or unusable names fall back to the rendered TDLib peer ID.
- Existing files are not migrated.
- Hashtag output behavior remains unchanged.
- Do not commit until manual review succeeds.
- Rebuild `tgdownloader.exe` after the fix.

---

### Task 1: Remove implicit author directories

**Files:**
- Modify: `internal/downloader/file.go`
- Test: `internal/downloader/downloader_test.go`

**Interfaces:**
- Consumes: `telegram.File.Metadata()` for hashtags only.
- Produces: `NewFile(file telegram.File, opts ...FileOption) File`, whose paths come only from explicit options and enabled hashtags.

- [ ] **Step 1: Write failing tests**

Add tests proving `NewFile` ignores `metadata["peername"]` and preserves explicit `WithSubdirs("Dialog")` plus hashtag behavior.

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/downloader -run "TestNewFile" -count=1`

Expected: author directory remains in `subdirs`.

- [ ] **Step 3: Implement minimal behavior**

Delete implicit `peername` insertion from `NewFile`. Keep `hashtags` handling gated by `WithSaveByHashtags`.

- [ ] **Step 4: Verify focused tests pass**

Run: `go test ./internal/downloader -run "TestNewFile" -count=1`

Expected: PASS.

### Task 2: Select one dialog directory in command workflows

**Files:**
- Modify: `cmd/cmd/helpers.go`
- Test: `cmd/cmd/helpers_test.go`

**Interfaces:**
- Produces: `dialogDownloadDirectory(peer peers.Peer) string`.
- Produces: `downloadFiles(..., subdirs []string, opts downloadOptions) error`.
- Consumes: `downloader.WithSubdirs(subdirs...)` for each queued file.

- [ ] **Step 1: Write failing directory-name tests**

Test visible names containing `<>:"/\\|?*`, control characters, trailing spaces/dots, and an empty result. Assert valid names remain readable and empty names fall back to `renderer.RenderTDLibPeerID(peer.TDLibPeerID())`.

- [ ] **Step 2: Write failing workflow test**

Feed two files carrying different author metadata through the download workflow. Assert both paths start with `/downloads/<dialog>/` and no author directory is created.

- [ ] **Step 3: Verify tests fail**

Run: `go test ./cmd/cmd -run "TestDialogDownloadDirectory|TestDownloadFilesUsesDialogDirectory" -count=1`

Expected: missing helper or author-based paths.

- [ ] **Step 4: Implement directory selection**

Create a rune-based component sanitizer, trim trailing spaces/dots, and fall back to TDLib ID. Pass the result from history, watcher, and message entry points into `downloadFiles`; construct each queue item with `downloader.WithSubdirs(subdirs...)`.

- [ ] **Step 5: Verify focused tests pass**

Run: `go test ./cmd/cmd -run "TestDialogDownloadDirectory|TestDownloadFilesUsesDialogDirectory" -count=1`

Expected: PASS.

### Task 3: Regression verification and executable

**Files:**
- Modify only if verification exposes a defect.

- [ ] **Step 1: Format changed Go files**

Run: `gofmt -w cmd/cmd/helpers.go cmd/cmd/helpers_test.go internal/downloader/file.go internal/downloader/downloader_test.go`

- [ ] **Step 2: Run all checks**

Run: `go test ./...`

Run: `go vet ./...`

Run: `git diff --check`

- [ ] **Step 3: Rebuild executable**

Run: `go build -o tgdownloader.exe .`

- [ ] **Step 4: Stop for manual review**

Report changed paths and executable timestamp. Do not stage, commit, or push.
