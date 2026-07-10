# Changelog

## Unreleased

- Added revision-bound XML patch protocol v1 with exact content anchors,
  multi-hunk atomic application, and safe character-range edits.
- Added Git-backed immutable plan revisions, append-only restore, compact
  autosaves, and crash recovery journaling.
- Moved revision storage to durable per-user application data and migrated
  cache-backed histories on demand.
- Serialized multi-process revision writes with per-plan transactions and
  compare-and-swap Git heads.
- Made release and local reinstalls refresh an existing managed Codex skill
  atomically while preserving unmanaged custom skills.
- Added clear, per-comment in-progress states for /btw and section iteration,
  with duplicate-run guards and automatic cleanup after success or failure.
- Clear obsolete character selections when an iterate proposal resolves a
  comment, and keep the accepted revision comparison available to show or hide.
- Render GitHub-Flavored Markdown tables in review plans with preserved
  line-level comment anchors, alignment, inline formatting, and narrow-view
  scrolling.
- Show complete comment threads, including `/btw` Q+A, directly below their
  anchors or alongside their final anchored line; stacked flow prevents long,
  overlapping, and same-line comments from covering plan content.
- Removed the redundant sticky handoff preview panel; finalization retains the
  authoritative handoff review.

## v0.1.0

- Initial open-source release.
- Local browser review for Codex plans.
- Threaded inline feedback and private notes.
- Optional Codex app-server side questions.
- Focused section iteration with proposal diffs.
- Self-contained release binaries for Linux, macOS, and Windows.
- Licensed under GPLv3.
