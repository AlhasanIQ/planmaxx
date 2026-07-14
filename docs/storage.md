# Review Storage Contract

This document is the compatibility contract for PlanMaxx review persistence.
Do not weaken these rules without an explicit migration and tests.

## Ownership and location

- The Markdown or HTML plan file remains the source document. PlanMaxx never
  makes the browser or its working revision the source-file authority.
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
plan, revision metadata and Git commit IDs, comments and anchors, replies,
positions, side answers, promotions, digest, and pending proposal. It also
stores the canonical source path and last externally observed source text/hash
separately from the working review plan.

- Migrate known old schemas sequentially.
- Schema v3 stores `planFormat`; v1 and v2 records infer it from the canonical
  source path and default to Markdown when the path is ambiguous. Schema v4
  makes proposal lifecycle, immutable revision feedback, and validated session
  transitions load-bearing; older envelopes migrate before semantic repair.
- Reject a newer or unknown schema without rewriting it.
- Write via temp file, file sync, rename, then directory sync; retain 0600
  permissions.
- Preserve a record if parsing or migration fails. Never replace it with an
  empty session.
- Revision bodies live in one PlanMaxx-managed bare Git repository per user
  profile in durable application-data storage, never the OS cache. Each
  logical plan has a namespaced head ref and each accepted revision is an
  append-only source commit. New commits use `plan.source`; reads retain
  compatibility with older `plan.md` commits. Autosaves compact committed
  revision bodies and hydrate them from Git on reload. A cache-backed store
  from older builds is imported plan-by-plan without changing commit IDs.
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
  write the source file. Applying a proposal changes only the working
  review revision.

## Final-review iteration lifecycle

- Choosing **Iterate plan** stores a pending whole-plan proposal against the
  current revision. Proposal creation and refinement do not append a revision.
- While a proposal is pending, its feedback snapshot is frozen: comments,
  replies, kinds, anchors, `/btw` answers/promotions, revision restore, a second
  unrelated iteration, and finalization wait for Apply or Discard. Cancel
  remains available.
- Refining that proposal keeps the original final-review digest authoritative
  and continues to compare the complete pending plan, not only one patch hunk.
- **Apply as new revision** atomically appends an `iteration` revision, stores
  immutable snapshots of consumed decision threads, resolves those mutable
  threads, clears their obsolete text selections, resets consumed `/btw`
  promotions and any stored final digest, and removes the pending proposal.
- Discarding the proposal leaves comments, promotions, digest, plan, and
  revision history unchanged.
- Deliberately reopening a finalized or canceled autosave starts an `active`
  cycle and clears the prior terminal digest while preserving plan revisions
  and review history. A newer terminal generation observed by another already
  running server remains terminal and retains its digest.

## Review API projection

- `/api/state` is a versioned client projection, not the persisted `Session`
  record. Collections are always arrays, review phase and capabilities come
  from the backend, revision bodies are omitted, and pending proposals expose a
  lightweight summary plus an `activeChange`.
- Pending proposals and `/api/revisions/{from}/diff/{to}` use the same Go-owned
  change view: exact before/after document snapshots, stable rows, replacement
  clusters, comment placements, and immutable accepted-feedback placements.
- The browser renders complete before/after documents for Markdown context but
  does not compute diffs, reconstruct documents, or infer comment placement.

## CRDT boundary

PlanMaxx intentionally does not use a CRDT. External editors and agents write
ordinary source-file replacements, not CRDT operations, so a CRDT would create
a second conflicting source of truth. Use durable optimistic transactions for
review metadata and explicit external revisions for source-file changes.

## HTML rendering boundary

HTML Preview is derived and non-authoritative. It runs in an iframe without
script, same-origin, form, popup, download, or navigation permissions and with
a CSP that blocks network resources. Source mode owns comments, iteration, and
diff anchors. PlanMaxx never writes preview DOM back into the session, source
file, autosave, revision store, or handoff.
