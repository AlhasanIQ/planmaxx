# Product Contract

PlanMaxx is a local, blocking review boundary between an agent-written plan and
implementation. The plan file and the local review bundle remain authoritative;
an agent provider is optional.

## Support tiers

1. **Review and handoff:** Any caller that can run `planmaxx review` can pause,
   collect feedback, finalize the plan, and consume the stdout handoff.
2. **Assisted review:** Side questions and iteration are available only when
   PlanMaxx can identify and fork the caller's active agent session.
3. **Unavailable attachment:** PlanMaxx keeps ordinary review available and
   disables assisted actions. It does not substitute a fresh or copied-context
   agent run.

## Initial providers

- Codex attaches through `CODEX_THREAD_ID` and `codex app-server`.
- Local Claude Code's plain PlanMaxx skill substitutes `${CLAUDE_SESSION_ID}`
  into an invocation-only session flag, then uses a safe-mode, tool-disabled,
  non-persistent fork of that exact Claude session.

The skill-supplied Claude session ID takes precedence over ambient session
markers. For useful bare-command detection, PlanMaxx also reads
`CLAUDE_CODE_SESSION_ID`, which Claude Code injects into tool subprocesses, and
the former PlanMaxx-managed Claude marker remains a compatibility fallback.
Ambient detection does not replace the exact skill handoff: Anthropic
documents that ID-less `--continue` or `--resume` startup can leave
`CLAUDE_CODE_SESSION_ID` pointing at the initial startup ID. An executable
merely being present on `PATH` is not evidence that its context belongs to the
caller. Reviewers can explicitly select `codex`, `claude`, or `none`.

## Safety and presentation

- Assisted prompts run in validated disposable forks and must not mutate the
  workspace. Codex is read-only and network-disabled; Claude Code disables
  customizations and built-in tools for the child process.
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
