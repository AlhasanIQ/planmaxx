# Review Storage Contract

This document is the compatibility contract for PlanMaxx review persistence.
Do not weaken these rules without an explicit migration and tests.

## Ownership and location

- The Markdown plan file remains the source document. PlanMaxx never makes the
  browser or its working revision the source-file authority.
- Submitted review state is stored in `<plan-file>.planmaxx-review.json`.
  If that cannot be written, use the deterministic user-cache fallback for the
  same canonical plan path.
- Browser storage is non-authoritative and may contain preferences only.
- Do not delete review records automatically on cancel or approval.

## Document identity

- A review history belongs to the plan's **canonical logical path**. Symlink
  aliases resolve to one history.
- An atomic editor save that replaces the inode at that path is an external
  source revision, not a new history. Distinct hard-link paths are distinct
  logical plans.
- Content hashes are version tokens for detecting source edits; they are not
  document identity.

## Durable record

The versioned JSON envelope stores the complete review workspace: working
plan, revision metadata and Git commit IDs, comments and anchors, replies, positions, side answers,
promotions, digest, and pending proposal. It also stores the canonical source
path and last externally observed source text/hash separately from the working
review plan.

- Migrate known old schemas sequentially.
- Reject a newer or unknown schema without rewriting it.
- Write via temp file, file sync, rename, then directory sync; retain 0600
  permissions.
- Preserve a record if parsing or migration fails. Never replace it with an
  empty session.
- Revision bodies live in one PlanMaxx-managed bare Git repository per user
  profile in durable application-data storage, never the OS cache. Each
  logical plan has a namespaced head ref and each accepted revision is an
  append-only `plan.md` commit. Autosaves compact committed revision bodies
  and hydrate them from Git on reload. A cache-backed store from older builds
  is imported plan-by-plan without changing commit IDs.
- A short adjacent Git journal is written after a revision body commits and
  before its autosave metadata is replaced. On restart, PlanMaxx replays it
  only when the autosave generation still matches; a newer or conflicting
  record is never overwritten. Restoring history appends a new commit with the
  chosen body—it does not rewrite Git history or review metadata.

## Agent patch protocol

Every model-facing prompt includes `internal/prompts/templates/protocol.gotmpl`.
Section iteration uses protocol v1: a response is revision-bound and contains
one or more non-overlapping XML hunks with `before`, `expected`, `after`, and
`content`. PlanMaxx locates a unique exact content match in the base revision;
line/character hints are advisory and there is no proximity window. This lets
a small character selection safely drive an exact local edit or a coordinated
rename anywhere in the plan. Normal escaped XML is required by the prompt;
CDATA is accepted as ordinary XML text syntax but is unnecessary; the prompt
asks models to use escaped XML text instead.

## Server lifetime and concurrency

Servers are short-lived workers, not review owners. A killed server must not
lose any action that already succeeded.

- Every accepted state change is synchronously persisted before its success
  response.
- Records have a monotonic generation. A save holds a brief cross-process
  storage lock, compares its expected generation, then atomically writes the
  next generation.
- A per-logical-plan transaction lock spans Git commit/ref advancement,
  journal recovery, and autosave persistence. A shared repository-write lock
  serializes bare-repo initialization and object/ref mutation. Git heads use
  compare-and-swap semantics: a stale writer returns a conflict and cannot
  overwrite another PlanMaxx process's history.
- Multiple servers may review the same plan concurrently. A stale save returns
  a conflict; it must never overwrite the newer record. Clients reload the
  authoritative state and retry deliberately.
- Do not reintroduce a server-lifetime lock/lease: it breaks normal concurrent
  agent sessions and does not improve durability.

## Source-file edits and review features

Before any state-changing action, compare the on-disk source file with the
stored source baseline.

- If unchanged, keep the latest PlanMaxx working revision—even if it differs
  from the source file because a proposal was accepted in PlanMaxx.
- If changed by an editor or another agent, persist an `external` revision,
  retain all review artifacts, and require the client to refresh before retry.
- Reanchor a comment only for one unambiguous matching target. Preserve
  ambiguous or deleted comments and mark them `stale`; an explicit edit or
  reanchor reopens them.
- An external change makes a pending section proposal obsolete and discardable;
  it must not be silently applied or discarded.
- Comments, replies, side answers, promotions, and proposal creation do not
  write the Markdown source file. Applying a proposal changes only the working
  review revision.

## CRDT boundary

PlanMaxx intentionally does not use a CRDT. External editors and agents write
ordinary Markdown replacements, not CRDT operations, so a CRDT would create a
second conflicting source of truth. Use durable optimistic transactions for
review metadata and explicit external revisions for source-file changes.
