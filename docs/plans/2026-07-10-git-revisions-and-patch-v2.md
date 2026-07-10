# Git-backed revisions and content-anchored patch protocol

## Goal

Replace embedded review-revision bodies with commits in a PlanMaxx-managed,
bare Git repository, and replace positional section-iteration edits with
revision-bound, content-anchored patch hunks. Git owns immutable text history;
PlanMaxx retains review threads, anchors, proposals, and source-baseline
semantics.

## Scope and decisions

- Use `go-git` so the CLI remains self-contained and the on-disk repository is
  inspectable by normal Git tooling.
- Create one bare repository per PlanMaxx profile under the application data
  directory. Namespace one head ref per stable logical plan ID, serialize
  writes per plan, and compare-and-swap heads under a short repository lock.
- Each accepted plan revision is one commit containing a single Markdown blob;
  no worktree is created. Pending proposals stay in review metadata until the
  user applies them.
- Store Git commit IDs in review metadata instead of full revision bodies. Add
  a migration path for existing autosaves that still embed historical content.
- Keep the current Go diff renderer, loading its two inputs from Git commits.
- Make section iteration protocol v2. A proposal is bound to one base commit
  ID/revision and contains one or more non-overlapping hunks. Position hints
  are advisory only.

## Protocol v2

For the reviewer’s exact selection, the model uses `target="selection"` and
must echo the expected selected text. It never supplies character offsets.

For any complete-line change anywhere in the plan, a hunk supplies:

```xml
<replacement target="lines" start_hint="22" end_hint="22">
  <before>- Previous unchanged line</before>
  <expected>- Product: **Old Name**</expected>
  <after>- Next unchanged line</after>
  <content>- Product: **New Name**</content>
</replacement>
```

All content is ordinarily escaped XML; CDATA remains accepted solely for
backward compatibility. The server resolves the
unique `before + expected + after` hunk in the declared base content. It may
recover from incorrect numeric hints only when that match is unique; missing
or ambiguous context rejects the whole proposal. The server validates every
hunk, detects overlap, applies all hunks atomically from bottom to top, and
creates one proposed plan.

## Implementation steps

1. Add a `internal/revisions` Git-store abstraction with profile-path
   configuration, namespaced refs, commit/read/restore APIs.
   Add `go-git` and tests using temporary bare repositories.
2. Extend session persistence with logical plan ID, head commit ID, and
   Git-backed revision metadata. Migrate legacy embedded revision text on load
   without losing review history; use a small write journal/recovery record so
   ref advancement and autosave metadata cannot diverge silently.
3. Route review creation, external-source reconciliation, accepted immediate
   proposals, normal turns, revision history, and diff endpoints through the
   revision store. Retain source baseline behavior and pending-proposal
   obsolescence rules.
4. Implement protocol v2 in the dedicated protocol prompt, annotated model
   view, strict XML parser, and section-iteration service. Remove the allowed
   line window and positional character/line authority.
5. Replace single-anchor proposal application with a list of validated applied
   hunks and line deltas. Preserve the initiating thread’s result anchor;
   resolve included threads and stale/reanchor all other affected threads
   deterministically after atomic application.
6. Update the UI/types only where revision IDs or multi-hunk proposal details
   must be surfaced. Keep the existing proposed-plan diff approval flow.
7. Document storage, migration, recovery, and protocol v2 in README and
   `docs/storage.md`; update PRODUCT/ROADMAP because durable shared Git
   history becomes product scope.

## Verification

- Unit tests for bare-repo creation, refs, commits, restore-as-append,
  migration, and journal recovery.
- Session/server tests for Git-backed revision loading, external edits,
  proposal application, multi-hunk shifts, stale/reanchor outcomes, and
  concurrent source changes.
- Protocol tests for escaped XML text, CDATA compatibility, protocol-v1
  rejection, incorrect hints with unique
  recovery, ambiguous/missing context rejection, boundary lines, repeated
  text, Unicode, non-overlap, and atomic no-mutation-on-failure behavior.
- `go test ./...`, `bun test`, `./scripts/build-web.sh`, and a live PlanMaxx
  review that approves an iteration and a restored historical revision.
