---
name: planmaxx
description: Use when an agent-written plan, design, or spec needs user review before implementation, or when the user asks to review, approve, or iterate on a plan.
---

<!-- planmaxx-managed-skill -->

# PlanMaxx

1. Write the plan to a Markdown or HTML file.
2. Run
   `planmaxx review --claude-session-id ${CLAUDE_SESSION_ID} <plan-file>`.
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
4. Treat stderr as status output and stdout as the user's next instruction.

Applying proposals updates PlanMaxx's review state. No plan file is written on
cancel; finalization is the only action that saves the finalized plan.

If the review command is interrupted or fails, do not read comments from the
bundle and apply them yourself, do not edit or commit the source plan, and do
not infer approval. Relaunch `planmaxx review` in the foreground so PlanMaxx
restores the review state, then keep waiting for an explicit final handoff.

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
