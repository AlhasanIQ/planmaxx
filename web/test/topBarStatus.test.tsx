import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AgentStatus, TopBar } from "../src/components/TopBar";

describe("AgentStatus", () => {
  test("identifies the main agent while no subagent is working", () => {
    const html = renderToStaticMarkup(
      <AgentStatus
        statusLabel="Review in progress"
        statusKind="idle"
        statusActor="planmaxx"
        agentDisplayName="Claude Code"
        agentAvailable
      />,
    );

    expect(html).toContain("Main agent");
    expect(html).toContain("Claude Code waiting");
    expect(html).toContain("Waiting for you to finish the review.");
    expect(html).not.toContain("Subagent");
  });

  test("foregrounds a running subagent and preserves the main-agent context", () => {
    const html = renderToStaticMarkup(
      <AgentStatus
        statusLabel="Iterating complete plan…"
        statusKind="busy"
        statusActor="subagent"
        agentDisplayName="Claude Code"
        agentAvailable
      />,
    );

    expect(html).toContain("is-subagent");
    expect(html).toContain("Subagent");
    expect(html).toContain("Claude Code running");
    expect(html).toContain("Iterating complete plan… Main agent is waiting for review.");
  });

  test("does not describe local work as subagent activity", () => {
    const html = renderToStaticMarkup(
      <AgentStatus
        statusLabel="Saving comment…"
        statusKind="busy"
        statusActor="planmaxx"
        agentDisplayName="Claude Code"
        agentAvailable
      />,
    );

    expect(html).toContain("PlanMaxx");
    expect(html).toContain("Working");
    expect(html).toContain("Main Claude Code is waiting.");
    expect(html).not.toContain("Subagent");
  });

  test("explains unavailable assistance and preserves manual-review capability", () => {
    const html = renderToStaticMarkup(
      <AgentStatus
        statusLabel="Review in progress"
        statusKind="idle"
        statusActor="planmaxx"
        agentDisplayName="Claude Code"
        agentAvailable={false}
        agentUnavailableReason="Claude Code did not provide an active session."
      />,
    );

    expect(html).toContain("is-unavailable");
    expect(html).toContain("Assistance off");
    expect(html).toContain("Manual review only");
    expect(html).toContain("Claude Code did not provide an active session.");
    expect(html).toContain("Agent-backed /btw and iteration are unavailable");
    expect(html).toContain("comments and manual review still work");
  });

  test("offers an explicit stop action while a restored iteration is running", () => {
    const noop = () => {};
    const html = renderToStaticMarkup(
      <TopBar
        statusLabel="Iterating complete plan…"
        statusKind="busy"
        statusActor="subagent"
        forIterationCount={2}
        privateCount={0}
        attentionCount={0}
        themeMode="system"
        resolvedTheme="light"
        onThemeModeChange={noop}
        currentRevisionId="rev-1"
        agentDisplayName="Claude Code"
        agentAvailable
        onOpenRevisions={noop}
        onCancel={noop}
        onCancelIteration={noop}
        onIterate={noop}
        onFinalize={noop}
        disabled={false}
        iterationRunning
      />,
    );

    expect(html).toContain("Stop iteration");
    expect(html).not.toContain("<span>Iterate</span>");
  });
});
