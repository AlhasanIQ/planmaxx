# Changelog

## v0.4.0 - 2026-07-18

- Added a document outline for Markdown and HTML plans, with heading-aware
  navigation that stays aligned to the current revision and review comparison.
- Improved review navigation so feedback and changed regions remain easy to
  traverse alongside the outline.

## v0.3.0 - 2026-07-15

- Replaced adjacent JSON autosaves and the shared revision repository with one
  user-scoped `.planmaxx` Git bundle per plan. Revisions, proposals, feedback,
  state history, and finalization checkpoints now use Git commits, refs, notes,
  and annotated tags, with atomic replacement and legacy import. Added an
  opt-in `--local-bundle` flag for keeping the bundle beside the plan.
- Added project-local legacy bundle migration, storage-kind-aware deprecated
  flag handling, `planmaxx doctor`, and verified `snapshot`/`export` bundles.
  Legacy runtime locks now live in temporary state instead of accumulating in
  repositories or Application Support.
- Added checksum-verified `planmaxx update` installs and a cached startup check
  that appends an agent-facing update notice to finalized review handoffs.
- Added explicit active, detached, and addressed comment states. Detached
  feedback can be reanchored or recorded on the revision that applied it.
- Added Previous/Next navigation across feedback and changed regions.
- Added whole-plan iteration proposals that create a revision only when applied.
- Added immutable revision feedback, revision comparisons, append-only restore,
  and Git-backed revision storage with crash recovery and concurrent-write
  protection.
- Added Markdown tables and HTML plans with safe Preview and exact Source review.
- Added exact, revision-bound XML patches for section iteration.
- Improved inline and alongside comment placement, filtering, progress states,
  selection handling, and revision line gutters.
- Increased the default Codex side-action timeout to 30 minutes.
- Added autosave migrations, state validation, comparison caching, and browser
  regression coverage.

## v0.1.0

- Initial release with local plan review, threaded feedback, private notes,
  Codex side questions, section iteration, proposal diffs, and self-contained
  binaries for Linux, macOS, and Windows.
