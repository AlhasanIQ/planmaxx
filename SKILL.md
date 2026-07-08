---
name: planmaxx
description: Use when you have drafted a plan, design, spec, or implementation plan and need user review before proceeding. Also use when the user asks to review, approve, reject, or iterate on an agent-written plan. Run PlanMaxx as the blocking review gate instead of asking the user to manually inspect pasted markdown.
---

<!-- planmaxx-managed-skill -->

# PlanMaxx

Use PlanMaxx after drafting a plan that needs user review before implementation.

## Workflow

1. Write the plan to a markdown file.
2. Run `planmaxx review --handoff-out /tmp/planmaxx-handoff.md <plan-file>`.
3. Wait for the command to finish.
4. Treat stderr as status output only. It includes the local review URL and autosave path.
5. Treat stdout, or the `--handoff-out` file, as the next instruction from the user.

## Outcomes

- Approved: continue from the final plan and approved review digest.
- Rejected: revise the plan using the rejection digest, then run PlanMaxx again for review.
- Canceled: stop. No handoff was produced.

Do not continue implementation after a rejected or canceled review.

## Useful Flags

- `--no-browser`: print the review URL without opening a browser.
- `--host <host>`: bind the local review server host. Default: `127.0.0.1`.
- `--port <port>`: bind a fixed port. Default: `0`, meaning a random available port.
- `--handoff-out <path>`: write the final handoff to a file as well as stdout.
- `--autosave-out <path>`: write recoverable review state to a specific file.
- `--side-question-timeout <duration>`: timeout for one Codex side question. Default: `45s`.

## Codex Side Actions

When `CODEX_THREAD_ID` is available, PlanMaxx may use Codex app-server for side questions and section iteration. Do not fake this context manually. If unavailable, PlanMaxx still supports normal review, comments, approval, rejection, and handoff.
