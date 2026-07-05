#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(scriptDir, "..");
const webRequire = createRequire(path.join(root, "web", "package.json"));
const artifactDir = path.join(root, "artifacts", "renders");

async function main() {
  const playwright = loadPlaywright();

  await mkdir(artifactDir, { recursive: true });
  const tempDir = await mkdtemp(path.join(tmpdir(), "planmaxx-render-"));
  const planPath = path.join(tempDir, "billing-export-rollout.md");
  await writeFile(planPath, realisticPlan(), "utf8");

  const child = spawn("go", ["run", "./cmd/planmaxx", "review", "--no-browser", planPath], {
    cwd: root,
    stdio: ["ignore", "pipe", "pipe"],
  });

  let finalized = false;
  const stdoutChunks = [];
  const stderrChunks = [];
  child.stdout.on("data", (chunk) => stdoutChunks.push(chunk));
  child.stderr.on("data", (chunk) => stderrChunks.push(chunk));

  try {
    const url = await waitForReviewURL(child, stderrChunks);
    await seedReview(url);
    await renderScreenshots(playwright.chromium, url);
    const draft = await requestJSON(url, "/api/digest/draft", {});
    draft.summary = "Reviewed billing export rollout with audit, rollback, and support readiness feedback.";
    await requestJSON(url, "/api/finalize", draft);
    finalized = true;
    const exitCode = await waitForExit(child);
    if (exitCode !== 0) {
      throw new Error(`planmaxx exited with ${exitCode}: ${Buffer.concat(stderrChunks).toString("utf8")}`);
    }
    const stdout = Buffer.concat(stdoutChunks).toString("utf8");
    if (!stdout.includes("audit logs include export requester")) {
      throw new Error("render scenario finalized without expected review feedback in handoff");
    }
    console.log(`Rendered ${path.join(artifactDir, "planmaxx-review-desktop.png")}`);
    console.log(`Rendered ${path.join(artifactDir, "planmaxx-review-mobile.png")}`);
  } finally {
    if (!finalized && child.exitCode === null) {
      child.kill();
      await waitForExit(child).catch(() => {});
    }
    await rm(tempDir, { recursive: true, force: true });
  }
}

function loadPlaywright() {
  try {
    return webRequire("playwright");
  } catch (error) {
    throw new Error(
      "Playwright is not installed. Run `cd web && bun install && bunx playwright install chromium` before rendering.",
      { cause: error },
    );
  }
}

function realisticPlan() {
  return `# Billing Export Rollout

## Context

Finance needs a CSV export for invoice reconciliation. Support needs the same
export for enterprise customer escalations, and security wants the export path
audited before rollout.

## Plan

1. Add the Cobra command and wire it into the existing root command.
2. Validate account, date range, and requester inputs before building the query.
3. Stream rows to a temporary file so large customers do not exhaust memory.
4. Ensure audit logs include export requester, account ID, date range, and row count.
5. Add a guarded rollout switch so the export can be enabled gradually.
6. Document rollback steps for disabling the command and revoking generated files.
7. Add unit tests for validation and an E2E smoke test for a representative export.
`;
}

async function waitForReviewURL(child, stderrChunks) {
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    const stderr = Buffer.concat(stderrChunks).toString("utf8");
    const match = stderr.match(/^PlanMaxx review URL: (.+)$/m);
    if (match) {
      return match[1].trim();
    }
    if (child.exitCode !== null) {
      throw new Error(`planmaxx exited before serving review UI: ${stderr}`);
    }
    await sleep(50);
  }
  throw new Error(`timed out waiting for review URL: ${Buffer.concat(stderrChunks).toString("utf8")}`);
}

async function seedReview(url) {
  const auditThread = await requestJSON(url, "/api/threads", {
    anchor: { startLine: 13, endLine: 14 },
    body: "Verify audit logs include export requester before finance starts using this.",
  });
  await requestJSON(url, `/api/threads/${encodeURIComponent(auditThread.id)}/reply`, {
    body: "Support also needs row count in the handoff so escalation reports can be traced.",
  });

  const rolloutThread = await requestJSON(url, "/api/threads", {
    anchor: { startLine: 15, endLine: 16 },
    body: "Add a rollback owner and expected time to disable the feature flag.",
  });
  await requestJSON(url, `/api/threads/${encodeURIComponent(rolloutThread.id)}/move`, {
    x: 760,
    y: 332,
  });
}

async function renderScreenshots(chromium, url) {
  let browser;
  try {
    browser = await chromium.launch();
  } catch (error) {
    throw new Error(
      "Chromium is not installed for Playwright. Run `cd web && bunx playwright install chromium` before rendering.",
      { cause: error },
    );
  }

  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 980 } });
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForSelector(".thread", { timeout: 5_000 });
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({
      path: path.join(artifactDir, "planmaxx-review-desktop.png"),
      fullPage: true,
    });

    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload({ waitUntil: "networkidle" });
    await page.waitForSelector(".thread", { timeout: 5_000 });
    await page.fill("#thread-filter", "rollback");
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({
      path: path.join(artifactDir, "planmaxx-review-mobile.png"),
      fullPage: true,
    });
  } finally {
    await browser.close();
  }
}

async function requestJSON(baseURL, pathname, body) {
  const response = await fetch(new URL(pathname, baseURL), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`${pathname} returned ${response.status}: ${text}`);
  }
  return text ? JSON.parse(text) : {};
}

async function waitForExit(child) {
  if (child.exitCode !== null) {
    return child.exitCode;
  }
  return new Promise((resolve) => {
    child.once("exit", (code) => resolve(code));
  });
}

function sleep(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

await main();
