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

If you use the Claude Code skill, refresh its installed copy after an update
using the same scope as the original install:

```bash
# User-wide
planmaxx skill install --target claude

# Repository-local
planmaxx skill install --target claude --repo /path/to/repo
```

Released builds check GitHub for updates at review startup at most once every
24 hours. If one exists, the final handoff tells the calling agent to notify
you and use the update command. Check failures never block review. Set
`PLANMAXX_NO_UPDATE_CHECK=1` to disable automatic checks.

### Agent Skills

Install the optional skill for the agent you use:

```bash
# Codex
bash -c 'set -o pipefail; curl -fsSL https://github.com/AlhasanIQ/planmaxx/releases/latest/download/install.sh | bash -s -- --install-codex-skill'

# Claude Code
bash -c 'set -o pipefail; curl -fsSL https://github.com/AlhasanIQ/planmaxx/releases/latest/download/install.sh | bash -s -- --install-claude-skill'
```

Codex uses the shared Agent Skills location:

- **User-wide (default):** `planmaxx skill install` installs the skill at
  `~/.agents/skills/planmaxx/`; remove it with `planmaxx skill remove`.
- **Repository-local:** `planmaxx skill install --repo /path/to/repo` installs
  it at `/path/to/repo/.agents/skills/planmaxx/`; remove it with
  `planmaxx skill remove --repo /path/to/repo`.

Local Claude Code uses a standard plain skill:

- **User-wide:** `planmaxx skill install --target claude` installs
  `~/.claude/skills/planmaxx/SKILL.md` (or
  `$CLAUDE_CONFIG_DIR/skills/planmaxx/SKILL.md` when configured).
- **Repository-local:** add `--repo /path/to/repo` to install under that
  repository's `.claude/skills/planmaxx/SKILL.md`.

Claude can invoke the skill when its description matches the task, or you can
invoke it directly with `/planmaxx`. Claude Code substitutes the exact current
session ID into the skill's command:

```text
planmaxx review --claude-session-id ${CLAUDE_SESSION_ID} <plan-file>
```

The hidden session flag takes precedence over ambient session markers. It is
invocation-only: the skill installs no plugin, hook, `SessionStart` setup, or
persistent environment change. Codex and other callers continue to use the
bare `planmaxx review <plan-file>` command.

Claude Code also injects `CLAUDE_CODE_SESSION_ID` into Bash and PowerShell tool
subprocesses, which lets PlanMaxx auto-detect useful bare commands launched
from Claude. Anthropic notes that after `claude --continue` or `claude
--resume` without an explicit ID, this environment value may still be the
initial startup ID. Use the installed skill command above when exact
current-session attachment matters. The `planmaxx` binary must be available on
the local Claude Code session's `PATH`.

Claude Code detects changes inside an existing top-level skills directory
without a restart. Restart a running session only when the install created that
top-level directory after the session started. Removal uses the same scope and
`--target claude`. Assisted actions require local Claude Code 2.1.214 or newer.
See Claude Code's official [skills documentation](https://code.claude.com/docs/en/slash-commands)
and [environment-variable reference](https://code.claude.com/docs/en/env-vars).

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

## Agent integrations

Normal review, comments, approval, and handoff do not require an agent
integration. Side questions and section or whole-plan iteration require a
safely attached active session:

| Agent | Attachment | Assisted-action behavior |
| --- | --- | --- |
| Codex | `CODEX_THREAD_ID` | Uses `codex app-server` and an ephemeral thread fork |
| Claude Code | Skill passes `${CLAUDE_SESSION_ID}` through an invocation-only flag; ambient `CLAUDE_CODE_SESSION_ID` is a fallback | Uses a safe-mode, tool-disabled, non-persistent session fork |

Detection is automatic. Use `planmaxx review --agent codex`,
`--agent claude`, or `--agent none` to override it. If attachment is missing or
fails, PlanMaxx disables assisted actions instead of silently sending copied
context to a fresh agent session. See the
[agent integration contract](docs/agent-integrations.md) for selection,
permissions, and adapter requirements.

## Privacy

The server binds to `127.0.0.1` by default and stores review state locally.
Agent-assisted actions send their prompt through a fork of the selected active
agent session. Claude Code forks run in safe mode with built-in tools disabled,
`dontAsk`, and session persistence off; Codex forks use approval policy `never`
inside a read-only, network-disabled sandbox. Provider authentication and the
source session's transcripts remain owned by the locally installed agent.
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
