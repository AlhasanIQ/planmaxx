# Review Storage Contract

Changes to this contract require migration coverage and tests.

## Authority and identity

- The Markdown or HTML file is the source document. The browser, preview, and
  working revision are not source-file authority.
- A plan's complete review workspace is stored in one `.planmaxx` Git bundle.
  No default file is written beside the plan and no shared revision repository
  is created.
- The default location is user-scoped durable application state:
  - Linux and other XDG systems: `$XDG_STATE_HOME/planmaxx/reviews`, or
    `~/.local/state/planmaxx/reviews`.
  - macOS: `~/Library/Application Support/PlanMaxx/reviews`.
  - Windows: `%LOCALAPPDATA%\PlanMaxx\reviews`.
- The bundle filename is the full SHA-256 of the canonical plan path. This
  keeps paths stable without exposing project or plan names in the state
  directory.
- `planmaxx review --local-bundle <plan-file>` opts into storing
  `<plan-file>.planmaxx` beside the canonical plan instead.
- Browser storage holds preferences only. Review records survive cancel and
  approval.
- The canonical path identifies a plan. Symlink aliases share history;
  distinct hard-link paths do not. Replacing content at the same path creates
  an external revision. Content hashes detect edits but do not identify plans.

## Persistence

PlanMaxx materializes a bundle into a disposable bare repository while a review
is open. Every mutation builds and verifies a replacement bundle in a fresh
repository, then atomically replaces the one durable file with mode `0600`.
Temporary repositories and lock files are runtime implementation details, not
additional durable records.

The protocol uses these Git refs:

| PlanMaxx concept | Git representation |
| --- | --- |
| Accepted revision history | Linear commits at `refs/heads/revisions`; each tree contains `plan.source` |
| Pending proposal | A commit parented to its base revision at `refs/heads/proposal` |
| Review workspace metadata and source baseline | Append-only commits at `refs/heads/state`, containing `review.json` and `source-baseline.source` |
| Immutable feedback consumed by a revision | Git notes at `refs/notes/feedback` |
| Finalized review checkpoint | Annotated tag under `refs/tags/finalized/` with the final digest |

`review.json` stores the domain data Git does not understand: thread lifecycle,
anchors, replies, private side answers, proposal metadata, format, digest, and
schema version. Revision bodies are omitted from that JSON once their Git commit
exists and are hydrated from commits on load.

- Use native Git plumbing for blobs, trees, commits, parentage, refs, notes,
  tags, reachability, bundle creation, bundle verification, and object import.
- Migrate known metadata schemas in order. Reject unknown newer schemas without
  rewriting them. Never replace an unreadable bundle with an empty session.
- Restore history by reading the selected commit and appending a new commit;
  never reset or rewrite accepted history.
- On first open only, import matching legacy JSON sidecars/cache records,
  project-local `.planmaxx/revisions.bundle` history, and reachable commits
  from the former shared bare repository. Reconstruct visible revisions when
  only the project bundle remains, preserve existing commit IDs exactly, and
  leave every legacy file untouched.
- The hidden deprecated `--autosave-out` alias inspects an existing target. A
  current bundle reopens normally; JSON or a legacy revision bundle is imported
  into the default user-state bundle. `--bundle-out` rejects legacy inputs with
  an actionable migration command instead of attempting to parse JSON as Git.

## Concurrency

- Persist each accepted mutation before returning success.
- Bundle state uses monotonic generations, a short per-plan cross-process lock,
  and compare-and-swap against the bundled `state` ref OID. A stale writer
  returns a conflict instead of overwriting another process.
- File sync, atomic rename, and directory sync make the entire multi-ref Git
  update visible as one durable-file replacement.
- Multiple servers may review one plan. On conflict, reload and retry.
- Runtime lock markers live under the temporary `planmaxx-locks` directory,
  including locks used while reading former shared repositories. They are not
  written beside plans or inside durable application state. Never unlink a
  lock marker as a cleanup strategy while processes may be using it.

## Diagnostics and snapshots

- `planmaxx doctor <plan>` verifies the current bundle, reports generation and
  status, probes the active write lock, lists matching review processes and
  legacy storage, and gives conservative migration or cleanup guidance. It
  never deletes review data.
- `planmaxx snapshot <plan> --out <file.planmaxx>` (also available as
  `planmaxx export`) atomically copies and verifies the current bundle. It
  refuses to overwrite an existing snapshot unless `--force` is supplied.
- Both commands accept `--bundle <path>` for reviews stored outside the default
  user-state location, including reviews created with `--local-bundle`.
- Prefer bundle snapshots over timestamped JSON copies. A snapshot contains the
  complete reachable revision, proposal, notes, state, and tag graph.

## Feature-to-mechanism map

| Product feature | Mechanism |
| --- | --- |
| Source-file identity and external-edit detection | Filesystem canonicalization plus hand-rolled SHA-256 baseline policy |
| Revision bytes, identity, ancestry, proposal base, restore | Native Git commits, trees, refs, and object reads |
| Complete single-file persistence and portability | Native `git bundle` plus atomic filesystem replacement |
| Feedback history attached to accepted revisions | Domain snapshots in metadata plus Git notes |
| Final approval checkpoints | Domain digest plus annotated Git tags |
| Revision and proposal comparison UI | Git-owned input bytes; Go comparison/model library; React rendering |
| Exact section proposal application | XML protocol/parser plus hand-rolled revision and anchor safety rules |
| Threads, replies, intent, active/detached/addressed lifecycle | Hand-rolled PlanMaxx domain model in versioned metadata |
| Anchor mapping across revisions | Hand-rolled conservative mapping; Git line numbers are not durable identity |
| Concurrent writers | OS file lock, Git ref OID compare-and-swap, atomic bundle replacement |
| Markdown/HTML source and safe preview | Parser/rendering libraries plus sandbox policy |
| Side questions and iteration generation | Codex app-server client plus PlanMaxx prompt/domain adapters |
| Browser preferences | Browser local storage only; never review authority |

Git deliberately does not decide whether feedback is addressed, whether an
anchor is safe, whether a proposal is obsolete, or whether a review may
finalize. Those are product semantics and remain validated by PlanMaxx.

## Source changes and anchors

Before each mutation, compare the source file with the stored baseline.

- If unchanged, keep the latest PlanMaxx working revision.
- If changed, append an `external` revision, preserve review state, require a
  client refresh, and mark any pending section proposal obsolete.
- Comments, side answers, and proposal creation do not write the source file.
  Applying a proposal changes only the working revision.
- Finalization writes the working revision to the source file by default, then
  records that exact content as the bundle baseline. `--save-to-file` writes an
  alternate plan file and leaves the original source baseline unchanged.

An anchor belongs to one revision; a line number is not durable identity.

- Map an anchor across one revision edge only when its source coordinate still
  contains the selected text and that text has one unique destination, or when
  an exact applied patch provides the result coordinate.
- Modified, duplicated, ambiguous, deleted, or already-drifted anchors detach.
  Never copy raw line numbers forward as proof.
- Editing or reanchoring detached feedback makes it active. If a reviewer
  confirms that a revision applied it, store an immutable snapshot on that
  revision and mark it addressed.
- Addressed feedback is immutable. Multi-revision feedback remains unplaced
  unless its mappings compose safely. Replacement content is a new anchor.

## Iteration

- **Iterate** creates a pending whole-plan proposal without adding a revision.
- While pending, its feedback snapshot is frozen until Apply or Discard.
- **Apply as new revision** appends an `iteration` revision, records consumed
  feedback, marks it addressed, normalizes related side answers, clears the
  final digest, and removes the proposal.
- Discard leaves the plan, feedback, digest, and history unchanged.
- Reopening a completed bundle preserves history and starts a new active cycle.

## API and preview

- `/api/state` is a versioned client projection. Go owns lifecycle, intent,
  capabilities, counts, diffs, placements, and review navigation.
- HTML Preview is sandboxed and derived from source. Preview DOM is never saved
  to the plan, bundle, revision history, or handoff.
