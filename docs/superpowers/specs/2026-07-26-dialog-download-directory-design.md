# Dialog Download Directory Design

## Problem

Downloaded files currently use the message author's visible name as their
subdirectory. In group histories this creates one directory per participant at
the root of the configured download directory.

## Desired Behavior

Every history download stores files under one directory belonging to the
selected dialog:

```text
downloads/<dialog name>/<file>
```

Message authors do not affect output paths. Files from different authors in the
same group therefore share the same dialog directory.

## Directory Name

The command layer derives the directory from the selected peer's visible name.
It sanitizes characters that are invalid in Windows path components and removes
control characters. If sanitization produces an empty name, the rendered TDLib
peer ID is used as the directory name.

## Data Flow

`downloadFilesFromPeer` resolves the dialog directory once and passes it through
the download workflow. Each `downloader.File` receives that directory explicitly
with `downloader.WithSubdirs`. `downloader.NewFile` no longer treats author
metadata as an implicit output directory.

Hashtag directories remain optional. When enabled, the existing additional
hashtag output paths are preserved; this change only replaces implicit author
directories with the explicit dialog directory.

The downloader manifest continues to resolve repeated filenames and stable file
identities relative to the configured output root.

## Compatibility

Existing files in author-named directories are not moved. A subsequent download
uses the new dialog directory and its manifest entries. Existing author metadata
may remain available for non-path purposes but must not select output paths.

## Tests

- Files from different authors in one group use the same dialog directory.
- Invalid or empty dialog names produce valid deterministic directory names.
- Duplicate filename and manifest behavior remains unchanged within the dialog
  directory.
- Hashtag mode still produces the dialog copy plus existing hashtag copies.
