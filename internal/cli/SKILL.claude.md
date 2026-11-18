---
name: planmaxx
description: Use when an agent-written plan, design, or spec needs user review before implementation, or when the user asks to review, approve, or iterate on a plan.
argument-hint: "[plan-file]"
---

<!-- planmaxx-managed-skill -->

# PlanMaxx

1. Use the supplied Markdown or HTML plan file. If the plan has no file yet,
   write it to one before starting the review.
2. Run
   `planmaxx review --claude-session-id ${CLAUDE_SESSION_ID} <plan-file>`.
   Run it from the current Claude project directory and quote the plan path
   when it contains spaces or shell metacharacters.
   Claude Code substitutes `${CLAUDE_SESSION_ID}` with the active session ID
   when invoking the skill. This is invocation-only: do not install or rely on
   hooks, `SessionStart`, persistent environment variables, or plugins.
   On finalization, PlanMaxx writes the finalized plan back to that source file
   and prints the approved handoff to stdout. Use
   `--save-to-file /tmp/final-plan.md` to write only the finalized plan content
   to a different file instead.
3. Run the review command in the foreground and wait for it to finish. Never
   launch it as a background task: the blocked foreground command is what keeps
   this agent from changing the plan while the user is still reviewing it.
   Request a foreground tool timeout of at least 600000 milliseconds if available.
4. Treat stderr as status output and stdout as the user's next instruction.

Applying proposals updates PlanMaxx's review state. No plan file is written on
cancel; finalization is the only action that saves the finalized plan.

If PlanMaxx reports `review canceled`, stop even though the command exits
with a nonzero status. If the review is interrupted or fails before an outcome,
leave the source plan alone and relaunch the same command in the foreground to
restore the review state. Wait for an explicit final handoff before applying
any review feedback.

Outcomes:

- Approved: continue from the reviewed plan and digest.
- Iterated: wait while the user reviews the proposal.
- Canceled: stop; no handoff was produced.

Useful flags:

- Review state defaults to one user-scoped `.planmaxx` bundle. Add
  `--local-bundle` to keep `<plan-file>.planmaxx` beside the plan instead.
- `--no-browser`: print the URL without opening the browser. The browser is
  opened by default.
- `--save-to-file <path>`: write only the finalized Markdown or HTML plan to
  this path instead of the source plan. The handoff still goes to stdout. The
  file is written only after approval; canceling writes no plan file.

The invocation associates the review with this active Claude Code session.
Plain review, comments, approval, and handoff remain available without
host-agent context.
