# PlanMaxx

PlanMaxx is a local review app for Codex plans. It opens a browser review
session, lets you comment on and revise the plan, then returns a clean handoff
for the next Codex turn.

## Install

PlanMaxx is distributed as a self-contained binary. Go, Bun, and Node are not
required for users.

```bash
bash -c 'set -o pipefail; curl -fsSL https://raw.githubusercontent.com/AlhasanIQ/planmaxx/main/install.sh | bash'
```

By default the installer puts `planmaxx` in `~/.local/bin`.

```bash
planmaxx version
```

## Quick Start

```bash
planmaxx review path/to/plan.md
```

PlanMaxx starts a local server on `127.0.0.1`, opens your browser, and blocks
until you approve, reject, or cancel the review.

On approval or rejection, the command prints the reviewed plan and review digest
to stdout. That output is meant to be pasted or returned directly to Codex.

## Screenshots

![PlanMaxx desktop review workspace](docs/screenshots/review-desktop.png)

The review workspace keeps the plan, anchored comments, revision history, and
handoff preview visible in one local browser session.

<p>
  <img src="docs/screenshots/handoff-preview.png" alt="PlanMaxx handoff preview" width="360">
  <img src="docs/screenshots/thread-card.png" alt="PlanMaxx annotated thread card with btw answer" width="320">
</p>

The smaller crops show the live handoff preview and a thread with an ephemeral
`/btw` answer that can be promoted into the next Codex handoff.

## What It Does

- Renders long plans in a readable local review UI.
- Adds threaded comments anchored to lines or text ranges.
- Keeps private notes out of the final handoff.
- Lets you promote useful side-question answers into the handoff.
- Supports focused section rewrites and proposal diffs before final approval.
- Autosaves review state next to the plan file as
  `<plan-file>.planmaxx-review.json`, with a cache-directory fallback if that
  location is not writable.

## Codex Integration

Basic review works with any markdown plan file.

Side questions and section rewrites require a Codex app-server context. When
`CODEX_THREAD_ID` is available, PlanMaxx starts:

```bash
codex app-server --listen stdio://
```

If that context is unavailable, PlanMaxx disables agent-assisted side actions
instead of guessing from copied context.

## Privacy

PlanMaxx is local-first. The review server binds to `127.0.0.1` by default and
review state is stored in a local autosave file. Side questions and section
rewrites are sent through Codex only when the current Codex thread context is
available.

## Development

Requirements for contributors:

- Go 1.22+
- Bun

```bash
cd web && bun install
./scripts/build-web.sh
go test ./...
go vet ./...
cd web && bun test && bunx tsc --noEmit
./scripts/e2e-smoke.sh
```

The web UI is built into `internal/review/static/` and embedded into the Go
binary. That directory is generated and ignored. On a fresh clone, run
`./scripts/build-web.sh` before `go build` or `go test ./...`.

For UI screenshots, run `node scripts/render-review.mjs`.

## Release

Releases are built by GitHub Actions from version tags. Each release includes
Linux, macOS, and Windows archives, `checksums.txt`, and tagged source archives.

See [docs/release.md](docs/release.md).

## License

PlanMaxx is licensed under GPLv3. See [LICENSE](LICENSE).
