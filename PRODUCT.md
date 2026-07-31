# Product Contract

PlanMaxx is a local, blocking review boundary between an agent-written plan and
implementation. The plan file and the local review bundle remain authoritative;
an agent provider is optional.

## Review lifetime

- A review remains alive while at least one browser tab is connected.
- Starting a review with no browser connected, or later losing every connected
  tab, starts a one-hour orphan timeout. Connecting during that hour keeps the
  review alive.
- Orphan cleanup preserves the active review bundle, does not write the plan or
  emit an approval handoff, and tells the caller why the process stopped.
- `--orphan-timeout <duration>` changes the delay and
  `--orphan-timeout 0` disables automatic cleanup.
- Manual `--no-browser` workflows have the same one-hour connection window and
  can opt out when an indefinite wait is required.

## Support tiers

1. **Review and handoff:** Any caller that can run `planmaxx review` can pause,
   collect feedback, finalize the plan, and consume the stdout handoff.
2. **Assisted review:** Side questions and iteration are available only when
   PlanMaxx can identify and fork the caller's active agent session.
3. **Unavailable attachment:** PlanMaxx keeps ordinary review available and
   disables assisted actions. It does not substitute a fresh or copied-context
   agent run.

## Supported providers

- Codex attaches through `CODEX_THREAD_ID` and `codex app-server`.
- Local Claude Code's plain PlanMaxx skill substitutes `${CLAUDE_SESSION_ID}`
  into an invocation-only session flag, then uses a safe-mode, tool-disabled,
  non-persistent fork of that exact Claude session.
- Grok Build's native skill substitutes `${SESSION_ID}` into an invocation-only
  session flag. Assisted actions use a restricted named fork of that exact
  session in a temporary copy of the parent workspace and delete the isolated
  state after every request.

The skill-supplied Claude session ID takes precedence over ambient session
markers. For useful bare-command detection, PlanMaxx also reads
`CLAUDE_CODE_SESSION_ID`, which Claude Code injects into tool subprocesses, and
the former PlanMaxx-managed Claude marker remains a compatibility fallback.
Ambient detection does not replace the exact skill handoff: Anthropic
documents that ID-less `--continue` or `--resume` startup can leave
`CLAUDE_CODE_SESSION_ID` pointing at the initial startup ID. An executable
merely being present on `PATH` is not evidence that its context belongs to the
caller. The Grok skill likewise supplies its exact session at invocation time;
automatic selection deliberately ignores the hook-only `GROK_SESSION_ID`
variable. Reviewers can explicitly select `codex`, `claude`, `grok`, or `none`.

## Safety and presentation

- Assisted prompts run in validated disposable forks and must not mutate the
  workspace. Codex is read-only and network-disabled; Claude Code disables
  customizations and built-in tools for the child process. Grok Build copies
  the parent project into an isolated workspace at the same relative CWD,
  relocates project paths, excludes and validates away executable agent
  configuration, applies a fail-closed sandbox that denies the original
  project, disables web, memory, subagents, MCP, shell, and editing tools, and
  deletes its persisted named child and copied state after the operation.
- HTML plans remain source-authoritative while Preview is a first-class review
  surface. Rendered text selections and element targets map back to source
  anchors used by comments, side questions, iteration, revisions, and handoff.
- Preview removes authored scripts, event handlers, controls, and remote
  resources. A nonce-restricted PlanMaxx bridge runs in an opaque,
  network-blocked iframe solely to relay annotations and highlights.
- Attachment availability and operation capabilities come from the server.
- The UI describes the calling agent as waiting and the final handoff as ready;
  it does not claim the caller resumed before stdout is delivered.
- PlanMaxx initially integrates locally installed, user-authenticated CLIs. A
  hosted or multi-user agent execution service is outside the current scope.
