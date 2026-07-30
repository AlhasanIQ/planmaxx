# Agent Integration Contract

PlanMaxx separates the always-available local review workflow from optional
agent-assisted operations.

## Selection

`planmaxx review --agent auto` uses this precedence:

1. `PLANMAXX_AGENT`, when set to `codex`, `claude`, `grok`, or `none`.
2. The Grok or Claude skill's invocation-only session ID. In `auto` mode,
   supplying both is an error rather than an ambiguous selection.
3. Claude Code's subprocess `CLAUDE_CODE_SESSION_ID` marker.
4. The legacy PlanMaxx-managed `PLANMAXX_CLAUDE_SESSION_ID` marker.
5. Codex's `CODEX_THREAD_ID` marker.
6. No assisted provider.

An explicit `--agent` overrides automatic selection. Finding an executable on
`PATH` is never sufficient evidence that its session belongs to the caller.
For Grok and Claude, the invocation-only ID takes precedence over ambient
environment markers. Auto mode deliberately ignores `GROK_SESSION_ID`: Grok
documents it for hooks, not ordinary terminal tools, and PlanMaxx does not
install a hook. An explicit `--agent grok` can still consume that variable for
a caller that intentionally provides it.

## Context and permissions

All supported adapters create a disposable fork of the caller's session for
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

### Grok Build

- User installs live at `${GROK_HOME:-~/.grok}/skills/planmaxx/SKILL.md`;
  repository installs live at `.grok/skills/planmaxx/SKILL.md`.
- This is a native plain skill with no hook or session-start setup. Grok can
  load it automatically from its description, and users can invoke it directly
  with `/planmaxx`.
- Grok substitutes the exact active session ID into
  `planmaxx review --grok-session-id ${SESSION_ID} <plan-file>` only when the
  skill is invoked. PlanMaxx validates the UUID and prefers it over ambient
  markers. The hidden flag is a skill handoff, not a general interactive
  option.
- Auto mode does not inspect `GROK_SESSION_ID`. Grok guarantees that variable
  to hook subprocesses only; relying on it would make a stale exported value
  ambiguous with another active provider.
- Before each assisted operation, PlanMaxx copies the parent conversation into
  a temporary `GROK_HOME` and copies the parent session's Git tracked and
  non-ignored files into a temporary workspace. For non-Git directories it
  copies ordinary files. Agent configuration directories, project instruction
  files, `.mcp.json`, `.envrc`, and `.direnv` are excluded; workspace symlinks
  are rejected rather than followed. The staged conversation's absolute
  project paths are relocated to the copy, and the child starts at the same
  relative CWD as the parent session. PlanMaxx first uses Grok's ACP
  `_x.ai/session/fork` operation to relocate the recorded CWD structurally,
  then safely rewrites remaining repository-root paths in the isolated
  child's JSON/JSONL state. `/btw` and iteration can therefore inspect files
  throughout the copied repository without exposing the real workspace to
  mutation.
- Each assisted operation chooses a new UUID, creates that named child through
  `grok agent --no-leader stdio`, validates the exact source ID, parent CWD,
  child ID, and copied CWD, then runs `grok --cwd <isolated-parent-cwd>
  --prompt-file <0600-file> --resume <child> --output-format json --tools
  read_file,grep,list_dir --allow Read(<isolated-root>/**) --allow
  Grep(<isolated-root>/**) --deny MCPTool --permission-mode dontAsk --sandbox
  planmaxx-isolated --no-subagents --no-memory --disable-web-search --no-plan
  --max-turns 2 --verbatim`.
- The allowlist contains only file inspection operations because Grok 0.2.114
  has no true no-tools flag; an empty `--tools` value means no override and is
  unsafe. Strict sandboxing confines those tools to the isolated workspace and
  system paths. There is no shell or editing tool.
- `planmaxx-isolated` is a generated custom profile that extends Grok's strict
  sandbox and explicitly denies the original project root. Grok documents
  custom profiles with a deny path as fail-closed if kernel enforcement cannot
  be applied. Before the model runs, `grok inspect --json` must prove that no
  hook, skill, plugin, MCP/LSP server, custom agent, project instruction, or
  managed executable configuration was loaded.
- The isolated home contains the relocated conversation and authentication
  file, but none of the user's hooks, plugins, MCP configuration, or compatible
  Claude/Cursor configuration. The child receives a minimal environment that
  removes custom-agent, auth-command, workspace-override, loader, and log-path
  variables.
- Calls are serialized, honor cancellation, require a successful `EndTurn`
  JSON envelope, and verify the exact distinct child session ID.
- Grok automatically persists conversations and has no non-persistence flag.
  PlanMaxx therefore deletes the named child after every successful, failed, or
  canceled operation and removes the entire temporary home and workspace. A
  cleanup failure fails the operation and disables the attachment.
- Attachment requires Grok Build 0.2.114 or newer and feature-probes every
  isolation, ACP stdio, structured-output, and cleanup CLI surface. The xAI
  fork extension is also validated on every operation because it is not
  advertised by the standard ACP capability envelope.
- Assisted Grok attachment is enabled only on macOS and Linux, where Grok
  0.2.114 implements the required OS sandbox. Windows and other platforms fail
  closed while plain PlanMaxx review remains available.
- On macOS, Grok documents that sandbox child-network restrictions are not
  enforced. PlanMaxx exposes no shell tool or hook/config surface to this child
  and separately disables web, MCP, memory, and subagent surfaces.

These paths and substitutions follow Grok Build's official
[skills documentation](https://docs.x.ai/build/features/skills-plugins-marketplaces).
Fork behavior and cleanup follow its [session documentation](https://docs.x.ai/build/features/sessions);
the restricted invocation uses the documented [headless](https://docs.x.ai/build/cli/headless-scripting),
[permission](https://docs.x.ai/build/features/permissions), and
[sandbox](https://docs.x.ai/build/features/sandbox) controls.

Grok does not support nesting this custom OS sandbox inside a parent Grok
process that is already sandboxed. Such a parent remains safe: PlanMaxx fails
the assisted request closed and disables the attachment instead of retrying
without isolation. Plain review remains available.

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
