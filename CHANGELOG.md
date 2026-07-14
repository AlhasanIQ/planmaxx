# Changelog

## Unreleased

- Moved revision history into a top-bar picker that shows the checked-out
  revision, leaving the page sidebar exclusively for alongside comments and
  preventing the plan from being squeezed into an accidental third column.
- Unified pending-proposal and revision comparisons behind a versioned,
  backend-owned change model. Go now computes diff rows, change clusters,
  complete document snapshots, comment placement, and immutable feedback
  placement; the React UI only renders those projections.
- Added session invariant validation, typed proposal/revision transitions,
  ordered idempotent autosave compatibility migrations, and browser regression
  coverage for comment ordering, overlapping comments, and added table rows.
- Replaced the unmaintained line-diff dependency with `sergi/go-diff` behind a
  deterministic line adapter, and expanded deletion coverage for CRLF,
  adjacent trailing deletions, and newline-terminated documents.
- Made final-review iteration an explicit persisted lifecycle: whole-plan
  proposals remain whole-plan across refinements, applying one appends a labeled
  iteration revision, archives consumed decisions, resets consumed `/btw`
  promotions and stale digest state, and clearly tells the reviewer that no
  revision is created until Apply.
- Added `.html` and `.htm` plans with a scriptless, network-blocked Preview,
  exact Source review, format-aware autosaves and prompts, and HTML-fenced
  handoffs while preserving Markdown behavior.
- Increased the default Codex app-server timeout for `/btw` questions and
  iterations from 45 seconds to 30 minutes; the timeout remains configurable
  with `--side-question-timeout`.

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
  overlapping, and same-line comments from covering plan content, and the
  comment filter searches thread and `/btw` Q+A text.
- Moved alongside comment cards out of the Markdown render surface into a
  dedicated rail while preserving line alignment and expanded anchor rows.
- Redesigned in-place comments as connected, lightweight discussion blocks
  beneath their anchors, with clearer grouping for multiple threads.
- Removed the redundant sticky handoff preview panel; finalization retains the
  authoritative handoff review.
- Replaced rejection with whole-plan iteration from the final-review dialog.
  Iteration creates a proposal to review; only approval ends the review and
  sends a handoff.
- Show accepted-proposal and historical-revision diffs in the main Markdown
  editor, including rendered table rows; the revision rail is now only the
  comparison selector.
- Clearly group resolved and stale feedback as historical, disable further
  agent actions for it, and exclude any promoted `/btw` answers attached to it
  from future handoffs.
- Preserve native text selection when opening the convenience comment composer;
  an untouched new composer now disappears on click-away without saving a
  comment.
- Refined active comment cards into a compact feedback workflow and restyled
  resolved or stale threads as a distinct archival section with no active
  iteration controls.
- Preserve immutable snapshots of feedback used for accepted proposals and
  display that feedback next to direct revision changes; multi-revision
  comparisons group feedback by the intervening accepted revision.
- Made revision comparison responsive for long plans: compact normal state
  responses, bounded fast line diffing with precise small-hunk refinement,
  immutable comparison caches, concurrent post-apply refreshes, and a visible
  in-editor loading state.
- Reworked in-place and alongside comment styling into clean, standalone cards;
  removed the colored side-rail treatment and made active, note, resolved,
  stale, and `/btw` states distinct through restrained surfaces and labels.
- Automatically open a review with the checked-out revision compared to its
  direct parent when revision history exists.
- Fixed GFM table rendering when a table is followed by a blank line and a
  subsequent Markdown block.
- Made table-cell comments source-safe: their exact visible selection is kept
  as context, while the anchor covers the full table row(s) so rendered-cell
  offsets can never target the wrong Markdown characters. Code-block comments
  retain their exact character anchors.
- Redesigned revision-comparison gutters to show `before → current` line
  coordinates. Only current-revision rows can receive comments, so a removed
  row can never steal a comment intended for an added or shifted line.
- Corrected comparison gutter markers so removed rows show `−` and added rows
  show `+`, with wider columns and balanced padding for multi-digit lines.

## v0.1.0

- Initial open-source release.
- Local browser review for Codex plans.
- Threaded inline feedback and private notes.
- Optional Codex app-server side questions.
- Focused section iteration with proposal diffs.
- Self-contained release binaries for Linux, macOS, and Windows.
- Licensed under GPLv3.
