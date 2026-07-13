# HTML Plan Support

## Goal

Add first-class `.html` plan review without regressing Markdown. An HTML plan
must render as a document rather than escaped source, while every existing
PlanMaxx feature continues to operate against the exact original HTML source:
comments and selections, side questions, section iteration, proposals, diffs,
revision history and restore, autosave and external edits, digest/finalization,
handoff, cancel, and responsive review layouts.

This work does not turn PlanMaxx into a general-purpose browser. HTML plans are
review documents: authored layout and self-contained styles are supported, but
scripts, forms, navigation, and network-loaded resources are not.

## Product and compatibility decisions

1. Recognize `.html` and `.htm` as HTML. Continue treating `.md`, `.markdown`,
   and unknown extensions as Markdown so existing permissive CLI behavior does
   not break.
2. Support both complete HTML documents and HTML fragments. Preserve the
   original source byte-for-byte in the session and revision history; rendering
   is always a derived view.
3. Keep Markdown behavior and its current line-oriented renderer unchanged.
4. Default HTML reviews to a rendered document view. Provide an exact source
   view for all comments and agent actions, and use source diffs for proposals
   and revision comparisons.
5. Render HTML in a scriptless sandbox. Preserve safe document structure,
   classes, IDs, and self-contained CSS so model-generated plans retain their
   intended visual hierarchy. Block scripts, event handlers, forms, embedded
   browsing contexts, top-level navigation, and remote resource loads. Permit
   only explicitly supported URLs and data assets.
6. Keep the existing source anchor contract: one-based source lines and UTF-16
   character offsets. Do not add a rendered-DOM anchor protocol. Reviewers
   switch to Source to comment or iterate; Preview remains visual and read-only.

## Phase 1: Make the plan format explicit end to end

- Add a small shared `PlanFormat` type (`markdown` or `html`) without a broad
  terminology rename. Infer it from
  the canonical path and test `.html`, `.htm`, Markdown, unknown extensions,
  empty files, mixed-case extensions, and symlinks.
- Carry `planFormat` through `session.Session`, the review API, TypeScript
  types, autosave documents, revision operations, prompt builders, and handoff.
  Keep format stable for every revision of one logical plan.
- Bump the autosave schema and migrate older records by inferring the format
  from `PlanPath`, defaulting to Markdown when the path is missing or
  ambiguous. Test v1/v2 migration, future-version rejection, and reopening old
  Markdown reviews unchanged.
- Stop describing revision blobs as unconditionally `plan.md`. New commits
  should use the format-neutral `plan.source` name; reads and restores must
  retain a fallback for existing `plan.md` commits so no current history is
  orphaned.
- Include the format in client state and format-specific comparison cache keys
  where needed. Verify that canonical path identity, autosave sidecars, Git
  plan IDs, concurrency checks, and external-source hashing remain based on the
  source document, not the rendered output.

Likely files: `internal/planfile/*`, `internal/session/*`,
`internal/review/autosave*`, `internal/review/server*`,
`internal/revisions/*`, `internal/cli/review*`, `web/src/types.ts`, and
`web/src/api.ts`.

## Phase 2: Add a safe HTML Preview and exact Source mode

- Render raw HTML in a `srcdoc` iframe with a sandbox that omits scripts,
  same-origin access, forms, popups, downloads, and navigation capabilities.
  Inject a restrictive CSP (`default-src 'none'`) before plan content; allow
  inline authored styles and data-only media while blocking scripts, frames,
  objects, forms, connections, and remote resources.
- Add Preview/Source controls. Preview is read-only and displays an explicit
  security/source notice. Source renders escaped HTML one physical line per
  row and reuses the existing comment, selection, highlight, side-question,
  iteration, inline/alongside, and thread-placement code without translation.
- Force Source while a proposal or revision comparison is visible. Applying,
  restoring, autosaving, and handing off always operate on the source string;
  Preview DOM is never serialized back into application state.
- Keep the iframe internally scrollable rather than adding an auto-sizing DOM
  bridge. This isolates authored CSS and avoids source maps, portal roots, and
  a second anchor model until concrete usage proves they are needed.

Likely files: `web/src/lib/htmlPreview.ts`, `web/src/lib/markdown.ts`,
`web/src/components/Plan.tsx`, `web/src/styles.css`, and unit tests.

## Phase 3: Make agent-assisted and handoff flows format-aware

- Add the plan format to the model-facing review XML and protocol template.
  Replace statements that every request contains a Markdown plan with
  format-neutral wording.
- For HTML section iteration, instruct the agent to edit exact HTML source,
  preserve a coherent document/fragment, return XML-escaped replacement
  content, and never translate it to Markdown. Keep the same revision-bound,
  exact-content patch protocol.
- Fence the approved final plan as `html` in handoff output and as `markdown`
  for Markdown. Ensure arbitrarily long backtick runs remain safe.
- Make side-question excerpts, selected text, digest context, full-plan
  iteration, and review annotations use original source coordinates and carry
  the correct `.html` path/reference.
- Validate proposals in exact source-diff mode. Applying, refining, discarding,
  comparing, and restoring a proposal must
  never serialize the preview DOM back over the source.

Likely files: `internal/prompts/*`, `internal/prompts/templates/*`,
`internal/reviewxml/*`, `internal/handoff/*`, `internal/digest/*`,
`internal/sectioniter/*`, and associated tests.

## Phase 4: Deterministic automated coverage

Add unit and integration coverage before browser E2E:

- Go: format detection; session/API round trips; autosave migration and
  recovery; HTML external edits and reanchoring; proposal apply/refine/discard;
  source diffs; Git commit/read/restore across legacy Markdown and new HTML
  blobs; prompt XML escaping; HTML handoff fencing; concurrent review conflicts.
- Web: CSP/sandbox document policy; exact source escaping and line retention;
  source selection round trips; highlights; Preview/Source toggles; notices;
  Markdown regression tests.
- Security fixtures must prove that scripts and event handlers never run,
  `/api` cannot be invoked from plan markup, forms/navigation are inert, and
  external images, fonts, CSS, imports, and fetch-like channels make no network
  requests. Also test malformed HTML and intentionally hostile CSS without
  allowing it to escape the sandbox or cover PlanMaxx controls.

Create a real Playwright suite rather than relying only on the current API-level
Go E2E tests. Use deterministic HTML fixtures and the existing fake Codex
app-server to exercise all user-visible features in a browser:

1. Open full-document, fragment, long, table-heavy, styled, minified, malformed,
   Unicode/entity, and hostile HTML plans; verify rendered and source views at
   desktop and mobile widths and in light/dark PlanMaxx themes.
2. In Source, create a line comment and an exact text-selection comment; adjust selection
   boundaries; switch inline/alongside modes; hover/focus; filter; edit; reply;
   change decision/note kind; re-anchor; resolve/stale through revisions; and
   delete.
3. Ask a side question, verify exact file/reference/selection context, promote
   and unpromote its answer, and cover unavailable and timeout states.
4. Generate a section proposal from a Source selection, inspect its source diff,
   refine it, apply it, discard another, compare revisions
   with feedback, and restore an older HTML revision.
5. Trigger final whole-plan iteration, review the resulting proposal, build the
   digest, inspect the handoff preview, finalize to stdout and `--handoff-out`,
   and separately verify cancel exits without a handoff.
6. Kill and reopen a review to prove autosave restoration; exercise unwritable
   sidecar fallback, an external HTML source edit, unique/ambiguous comment
   reanchoring, an obsolete pending proposal, and two-server conflict handling.
7. Verify keyboard shortcuts, inert link behavior, comment scrolling/placement,
   iframe scrolling, and no layout overlap for long content.
8. Run the existing Markdown browser/unit/API suites unchanged as the regression
   gate.

Add stable `data-testid` hooks where semantic locators are insufficient. Keep
the API-level E2E suite for backend lifecycle coverage, but make the Playwright
suite the authority for visual interaction and source-anchor behavior.

## Phase 5: Six live-model plans and visual acceptance review

After deterministic tests pass, generate six fresh standalone HTML plans: three
with Codex and three with `claude -p`. First record `codex --version`,
`codex --help`, and `claude --version` so the harness uses the installed CLI's
supported non-interactive invocation. Use the same provider-neutral base prompt:

> Produce a complete, implementation-ready plan as one standalone HTML file.
> Return only HTML, without Markdown fences or explanation. Use semantic HTML,
> a self-contained `<style>` block, no JavaScript, and no external assets. Make
> the plan visually structured with headings, callouts, tables, code/pre blocks,
> risks, testing, rollout, and open decisions.

Generate one plan per provider for each scenario, for six independent outputs:

1. Add passkey authentication and staged account migration to a multi-tenant
   SaaS product.
2. Move a regional event-processing pipeline to active-active operation with
   replay, backpressure, observability, and rollback.
3. Add an offline-first field inspection workflow with conflict resolution,
   media uploads, accessibility, and phased rollout.

Save the exact prompts, CLI/model versions, raw `.html` outputs, SHA-256 hashes,
screenshots, browser console output, and failed-network-request log under the
gitignored `artifacts/html-plan-validation/<run-id>/` directory. Do not silently
repair model output before opening it; rendering must be evaluated as generated.

For each of the six plans, open `planmaxx review <file>.html` and perform a
human visual pass at 1440x900 and 390x844:

- confirm typography, spacing, color contrast, headings, callouts, tables,
  code, long lines, overflow, and authored styling render cleanly;
- compare Rendered and Source views and confirm all meaningful source content
  remains discoverable;
- switch to Source, create one exact selection comment and one line comment, switch inline and
  alongside layouts, filter/focus them, and verify highlights and cards align;
- open a proposal source diff, then return without
  mutating the saved fixture;
- inspect the final handoff preview and confirm it contains the original HTML
  source with an `html` fence and correct review references;
- check browser console/CSP messages and prove the plan caused no unexpected
  network activity.

Record pass/fail and a short note for every plan. Any clipping, unreadable
content, source-anchor mismatch, unsafe execution/resource load, misplaced review
UI, or missing handoff content is a release blocker. Convert every discovered
class of failure into a deterministic fixture and automated regression test
before repeating the six-plan pass.

## Phase 6: Documentation and release gate

- Update `README.md` and both copies of the managed PlanMaxx `SKILL.md` to say
  plans may be Markdown or HTML and show `.html` usage. Keep the embedded and
  top-level skill byte-identical.
- Update `docs/storage.md` to use format-neutral source-document terminology,
  document the autosave migration and revision blob compatibility, and state
  that rendered HTML is never authoritative.
- Add HTML support to `ROADMAP.md` in the appropriate completed/current
  milestone when implementation lands. There is no `PRODUCT.md` in this
  repository, so no product file is created unless one is introduced first.
- Document the HTML security boundary, supported/stripped content, network
  policy, rendered/source modes, and visual-validation command.
- Run the complete gate after rebuilding embedded web assets:
  `./scripts/build-web.sh`, `go test ./...`, `go vet ./...`,
  `cd web && bun test && bunx tsc --noEmit`, the new Playwright HTML suite,
  `./scripts/e2e-smoke.sh`, the six-plan live visual review, and a final clean
  `git status` check. Do not commit `internal/review/static/` or generated
  validation artifacts.

## Acceptance criteria

- `planmaxx review plan.html` and `.htm` open in rendered HTML mode; Markdown
  reviews behave exactly as before.
- Full documents, fragments, styled plans, tables, code, Unicode/entities,
  minified markup, long plans, and malformed-but-browser-readable HTML remain
  reviewable.
- Every existing review feature listed in the browser E2E matrix works against
  original HTML source and survives autosave/revision/external-edit workflows.
- Proposal and history operations never derive source from the preview DOM;
  handoff returns the exact active HTML revision in an `html` fence.
- Hostile HTML cannot execute code, submit forms, navigate the app, call local
  APIs, escape its visual sandbox, or load remote resources.
- All automated gates pass, all six fresh Codex/Claude plans pass the documented
  visual checklist, and any issue found in that pass has a committed regression
  test before release.
