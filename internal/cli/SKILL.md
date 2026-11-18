---
name: planmaxx
description: Use when an agent-written plan, design, or spec needs user review before implementation, or when the user asks to review, approve, or iterate on a plan.
---

<!-- planmaxx-managed-skill -->

# PlanMaxx

1. Write the plan to a Markdown or HTML file.
2. Run `planmaxx review <plan-file>`.
   On finalization, PlanMaxx writes the finalized plan back to that source file
   and prints the approved handoff to stdout. Use
   `--save-to-file /tmp/final-plan.md` to write only the finalized plan content
   to a different file instead.
3. Run the review command in the foreground and wait for it to finish. Never
   launch it as a background task: the blocked foreground command is what keeps
   this agent from changing the plan while the user is still reviewing it.
4. Treat stderr as status output and stdout as the user's next instruction.

The review command needs to write its review bundle and listen for the browser
on loopback HTTP. Use `--local-bundle` if the default user state directory is
unwritable and the plan directory is writable. If the sandbox blocks the
listener, a required write, or app-server startup, request only the required
access for this foreground command through the current approval flow. If the
user runs the review in a host terminal instead, use `--agent codex` with
`CODEX_THREAD_ID` set to the exact existing thread ID. Use only a known ID.
The assisted Codex fork stays read-only with network access disabled.

Applying proposals updates PlanMaxx's review state. No plan file is written on
cancel; finalization is the only action that saves the finalized plan.

If PlanMaxx reports `review canceled`, stop even though the command exits
with a nonzero status. If the review is interrupted or fails before an outcome,
do not read comments from the bundle and apply them yourself, edit or commit
the source plan, or infer approval. Relaunch the same review command in the
foreground so PlanMaxx restores the review state and wait for the final handoff.

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

When Codex provides its active task identifier, PlanMaxx associates the review
with that session. Plain review, comments, approval, and handoff remain
available without host-agent context.
