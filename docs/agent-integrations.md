# Agent Integration Contract

PlanMaxx separates the always-available local review workflow from optional
agent-assisted operations.

## Selection

`planmaxx review --agent auto` uses this precedence:

1. `PLANMAXX_AGENT`, when set to `codex`, `claude`, or `none`.
2. The Claude skill's invocation-only session ID.
3. Claude Code's subprocess `CLAUDE_CODE_SESSION_ID` marker.
4. The legacy PlanMaxx-managed `PLANMAXX_CLAUDE_SESSION_ID` marker.
5. Codex's `CODEX_THREAD_ID` marker.
6. No assisted provider.

An explicit `--agent` overrides automatic selection. Finding an executable on
`PATH` is never sufficient evidence that its session belongs to the caller.
When Claude is selected, the invocation-only ID takes precedence over both
environment markers.

## Context and permissions

Both supported adapters create a disposable fork of the caller's session for
each operation.

### Codex

- Starts `codex app-server --listen stdio://`.
- Reads and forks `CODEX_THREAD_ID` with `ephemeral`, `excludeTurns`, and
  approval policy `never`, plus a read-only sandbox with network access off.
- Rejects any response that does not prove a distinct ephemeral fork and the
  requested approval and sandbox policy.
- Starts one turn in the fork and serializes app-server operations.
- On cancellation after a turn starts, interrupts the turn and drains both the
  interrupt response and terminal turn notification before reusing the
  protocol stream. A cleanup failure permanently disables the attachment and
  stops the app-server process.

### Claude Code

- PlanMaxx supports locally installed Claude Code. User installs live at
  `~/.claude/skills/planmaxx/SKILL.md` (or
  `$CLAUDE_CONFIG_DIR/skills/planmaxx/SKILL.md`); repository installs live at
  `.claude/skills/planmaxx/SKILL.md`.
- This is a plain Claude Code skill, not a plugin. It has no hooks or
  `SessionStart` setup. Claude can load it automatically when its description
  matches the task, and users can invoke it directly with `/planmaxx`.
- Claude Code substitutes its exact current session ID into the skill content,
  which launches
  `planmaxx review --claude-session-id ${CLAUDE_SESSION_ID} <plan-file>`.
  PlanMaxx validates the invocation-only ID and prefers it over ambient
  markers. The flag is hidden because it is a skill handoff, not a general
  interactive option.
- Claude Code independently sets `CLAUDE_CODE_SESSION_ID` in Bash and
  PowerShell tool subprocesses. PlanMaxx uses it for useful automatic
  attachment when a bare review command is launched from a Claude tool.
  Anthropic documents that `--continue` or `--resume` without an explicit ID
  may leave this variable set to the initial startup ID, so it is a fallback
  rather than the skill's exact-session handoff.
- The legacy `PLANMAXX_CLAUDE_SESSION_ID` marker remains accepted only so
  sessions started with an older PlanMaxx hook keep working during migration.
- Claude Code detects skill-file changes within an existing top-level skills
  directory during the current session. Restart is required only if that
  top-level directory was created after the session started.
- Each operation runs `claude -p --resume <session> --fork-session
  --safe-mode --no-session-persistence --output-format json --tools ""
  --permission-mode dontAsk`.
- Calls are serialized, use the review process working directory, honor the
  request context, and require a successful nonempty result envelope with a
  new session ID distinct from the source session.
- Attachment requires Claude Code 2.1.214 or newer and checks that the
  installed CLI advertises every required isolation flag. Safe mode disables
  user/project instructions, skills, plugins, hooks, MCP servers, and other
  customizations in the disposable child; no-session-persistence prevents
  saving that child transcript.

These paths, discovery rules, automatic invocation behavior, and live-change
semantics follow Claude Code's official
[skills documentation](https://code.claude.com/docs/en/slash-commands).
The subprocess session marker is documented in the official
[environment-variable reference](https://code.claude.com/docs/en/env-vars).
Claude cloud and Cowork sessions are outside this local CLI integration.

If attachment or process startup fails, PlanMaxx publishes the provider and
reason through `/api/state` but disables side questions and iteration. It never
falls back to a copied-context fresh run.

An attached provider is demoted after a non-cancellation request or protocol
failure. The browser refreshes server-authoritative capabilities and removes
assisted actions while normal local review remains available.

## Conformance requirements

Before adding another provider, validate:

- a trusted active-session identifier and a context-preserving disposable fork;
- structured terminal output distinct from progress and diagnostics;
- explicit non-mutating tool and permission controls;
- cancellation, timeout, process reaping, and serialization;
- exact PlanMaxx XML protocol output for side questions and iteration;
- user- and repository-scoped skill installation without overwriting unmanaged
  configuration;
- server-authoritative availability and provider-neutral browser behavior.

Provider-specific wire tests remain isolated from the shared prompt, proposal,
review, and handoff protocols.
