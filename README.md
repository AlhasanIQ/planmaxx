<p align="center">
  <img src="docs/planmaxx-icon.svg" width="112" alt="PlanMaxx logo">
</p>

<h1 align="center">PlanMaxx</h1>

<p align="center"><strong>Review and refine coding-agent plans before implementation.</strong></p>

PlanMaxx is a local review UI for Markdown and HTML plans. It supports anchored
comments, private notes and side questions, revision history, iteration, and an
approved handoff back to the agent.

## Install

```bash
bash -c 'set -o pipefail; curl -fsSL https://github.com/AlhasanIQ/planmaxx/releases/latest/download/install.sh | bash'
planmaxx version
```

The installer puts the PlanMaxx binary in `$HOME/.local/bin` by default.
Use `--install-dir` or `PLANMAXX_INSTALL_DIR` to change the location.
Review storage uses native Git, so `git` must also be installed and available
on `PATH`.

Update an installed release in place with:

```bash
planmaxx update
```

Released builds check GitHub for updates at review startup at most once every
24 hours. If one exists, the final handoff tells the calling agent to notify
you and use the update command. Check failures never block review. Set
`PLANMAXX_NO_UPDATE_CHECK=1` to disable automatic checks.

### Automatic Codex Skill

To install the optional Codex skill with the binary:

```bash
bash -c 'set -o pipefail; curl -fsSL https://github.com/AlhasanIQ/planmaxx/releases/latest/download/install.sh | bash -s -- --install-codex-skill'
```

Choose the scope that fits your workflow:

- **User-wide (default):** `planmaxx skill install` installs the skill at
  `~/.agents/skills/planmaxx/`; remove it with `planmaxx skill remove`.
- **Repository-local:** `planmaxx skill install --repo /path/to/repo` installs
  it at `/path/to/repo/.agents/skills/planmaxx/`; remove it with
  `planmaxx skill remove --repo /path/to/repo`.

## Quick Start

Ask your agent to use PlanMaxx, or tell the agent to "use planmaxx". For an
existing plan, run:

```bash
planmaxx review path/to/plan.md
planmaxx review path/to/plan.html
```

PlanMaxx opens a local browser and waits for one outcome:

- **Finalize** approves the current plan and emits its handoff.
- **Iterate** creates a proposal to review before it becomes a revision.
- **Cancel** exits without a handoff.

The approved handoff is always printed to stdout. On finalization, PlanMaxx
writes the finalized plan back to its source file by default. Pass
`--save-to-file <path>` to write only the finalized plan content to a different
file instead; the handoff prompt is never written there. No plan file is
written on cancel.

## Screenshots

In-place review keeps the proposed diff and its dedicated review thread in one
reading flow.

![PlanMaxx in-place review thread beneath a proposed diff](docs/screenshots/review-desktop.png)

Alongside review anchors a separate feedback card to its source line, while the
handoff preview makes the final agent context inspectable before approval.

<p>
  <img src="docs/screenshots/thread-card.png" alt="PlanMaxx alongside feedback card connected to line 14" width="320">
  <img src="docs/screenshots/handoff-preview.png" alt="PlanMaxx handoff preview" width="360">
</p>

## Review behavior

- Comments attach to exact source lines or text ranges.
- Active feedback can drive iteration or remain private.
- Detached feedback can be reanchored or recorded as addressed on the revision
  that applied it.
- Addressed feedback remains read-only revision history.
- The floating review queue moves through every feedback item and every changed
  region independently, with `Alt+↑` / `Alt+↓` keyboard navigation.
- The document outline follows Markdown headings and HTML headings or labelled
  sections, and opens HTML Source when a preview section is selected.
- `/btw` answers remain private unless explicitly included.
- Applying a proposal creates a revision; creating or refining one does not.
- The complete review workspace is one private `.planmaxx` Git bundle in the
  platform's user-state directory. It includes revision commits, a pending
  proposal ref, feedback notes, finalization tags, and versioned domain state;
  nothing is written beside the plan by default. Pass `--local-bundle` to keep
  `<plan-file>.planmaxx` beside the plan instead.

HTML opens in a scriptless, network-blocked Preview. Comments, iteration, and
diffs use Source mode so the original HTML remains authoritative.

## Storage tools

Inspect the bundle, active write lock, matching review processes, and any
legacy sidecars or revision stores:

```bash
planmaxx doctor path/to/plan.md
```

Create a verified, portable copy of the complete review workspace:

```bash
planmaxx snapshot path/to/plan.md --out review-backup.planmaxx
```

The `export` command is an alias for `snapshot`. Existing destinations require
`--force`. Legacy files are imported on the next review but are never deleted
automatically. Both storage commands accept `--bundle <path>` when a review was
created with a non-default bundle location.

## Codex

When `CODEX_THREAD_ID` is available, PlanMaxx uses `codex app-server` for side
questions and section iteration. Normal review and handoff work without it.

## Privacy

The server binds to `127.0.0.1` by default and stores review state locally.
Agent-assisted actions send their context through the active Codex task.
Released builds also make a cached request to the public GitHub Releases API at
review startup; set `PLANMAXX_NO_UPDATE_CHECK=1` to disable it.

## Development

Requires Go 1.22+ and Bun.

```bash
cd web && bun install
cd ..
./scripts/build-web.sh
go test ./...
go vet ./...
cd web && bun test && bunx tsc --noEmit
```

Build the UI before Go builds or tests. Generated files under
`internal/review/static/` are embedded in the binary and must not be committed.
See [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/release.md](docs/release.md).

Additional end-to-end and visual checks are available through
`scripts/e2e-smoke.sh`, `scripts/e2e-browser.sh`, and
`scripts/render-review.mjs`.

## License

GPLv3. See [LICENSE](LICENSE).
