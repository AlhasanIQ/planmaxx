# Changelog

## Unreleased

## v0.8.0 - 2026-07-31

- Added browser-presence cleanup for orphaned reviews. After the last connected
  review tab has been gone for one hour, PlanMaxx preserves the active bundle,
  stops the blocking review command, and returns an agent-facing explanation
  with instructions for `--orphan-timeout`; reconnecting cancels the countdown,
  and zero disables cleanup.

## v0.7.0 - 2026-07-30

- Added Grok Build assisted review through exact invocation-time `${SESSION_ID}`
  skill handoff and restricted forks of the active session. Each fork gets an
  isolated copy of the parent project at the same relative CWD, without agent
  hooks/configuration. Grok's ACP fork relocates conversation paths into the
  copy; the temporary session, home, and workspace are validated and deleted
  after use.
- Added native user and repository Grok skill installation, `GROK_HOME`
  support, automatic provider selection, capability checks for Grok Build
  0.2.114+, and end-to-end fork, cleanup, cancellation, and installer coverage.
- Made standalone CLI review a first-class mode: clean `auto` launches without
  an agent, `--agent none` overrides stale provider markers, local comments,
  replies, proposal decisions, approval, saving, and handoff remain available,
  and assisted controls and wording disappear when no session is attached.
- Added release-installer coverage that runs the installed binary through a
  complete standalone review from an arbitrary working directory. Startup
  update notices are now equally useful to manual callers and agents.
- Anchored rendered-HTML comment cards directly beneath their target in
  In-place view and beside the target's live iframe position in Alongside view.
  Inline cards and drafts now scroll within Preview instead of growing or
  scrolling the outer page.
- Added two-way navigation feedback for HTML comments: selecting a side card
  scrolls and pulses its Preview or Source anchor, while selecting highlighted
  rendered/source content pulses the matching card. Crowded alongside cards
  avoid overlap and retain an exact connector to the rendered target.
- Replaced repeated provider documentation with concise feature and install
  matrices for manual CLI, Codex, Claude Code, and Grok Build.

## v0.6.0 - 2026-07-30

- Made rendered HTML Preview a complete review surface with source-mapped text
  and element annotations, persistent highlights, rendered outline navigation,
  inline/alongside threads, and the existing comment, private-note, `/btw`, and
  section-iteration actions. Preview remains isolated and network-blocked, with
  only a nonce-restricted PlanMaxx annotation bridge allowed to execute.
- Added precise rendered-HTML element targeting on hover, including segmented
  borders for wrapped inline content, SVG and table geometry, element labels,
  and a distinct existing-comment state. Text-range selection suppresses the
  element target, exact element comments no longer collide with nested range
  comments, and native `<details>` toggling remains available.
- Synchronized HTML Preview and Source positions in both directions and kept
  the rendered iframe mounted across view switches, so moving between tabs no
  longer returns to the top. HTML Source now uses line-preserving structural
  indentation and Shiki highlighting without changing comment anchor offsets.
- Expanded browser coverage across Markdown and rendered HTML for in-place and
  alongside comments, repeated `/btw` questions, promoted answers, multi-step
  section refinement, lists, tables, code/diagram blocks, SVG, details, and
  responsive layouts. Fixed the mobile top bar and long source rows so review
  controls and plan content no longer create horizontal page overflow.

## v0.5.0 - 2026-07-30

- Added Claude Code side questions and iteration through context-preserving
  session forks. The plain Claude Code skill now passes its exact
  `${CLAUDE_SESSION_ID}` through an invocation-only flag; ambient
  `CLAUDE_CODE_SESSION_ID` remains available for bare-command auto-detection.
- Added explicit agent selection, server-authoritative assisted-action
  capabilities, provider-neutral UI language, and safer symlink-aware skill
  installation.
- Isolated Claude child runs with safe mode, disabled tools, and no transcript
  persistence; added Claude version/capability checks and managed skill
  upgrades. Claude installs now use the standard
  `.claude/skills/planmaxx/SKILL.md` layout and remove PlanMaxx-managed legacy
  plugin and hook components during migration.
- Updated the Codex app-server request to match the current generated schema,
  require a read-only network-disabled ephemeral fork, and interrupt/drain
  canceled turns before reusing the protocol stream, including terminal-status
  validation during cancellation cleanup.

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
