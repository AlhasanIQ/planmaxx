#!/usr/bin/env node
import { spawn } from "node:child_process";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(scriptDir, "..");
const webRequire = createRequire(path.join(root, "web", "package.json"));
const artifactDir = path.join(root, "artifacts", "renders");
const readmeScreenshotDir = path.join(root, "docs", "screenshots");

async function main() {
  const playwright = loadPlaywright();

  await mkdir(artifactDir, { recursive: true });
  await mkdir(readmeScreenshotDir, { recursive: true });
  const tempDir = await mkdtemp(path.join(tmpdir(), "planmaxx-render-"));
  const planPath = path.join(tempDir, "billing-export-rollout.md");
  const fakeBinDir = path.join(tempDir, "bin");
  await writeFile(planPath, realisticPlan(), "utf8");
  await writeFakeCodex(fakeBinDir);

  const child = spawn("go", ["run", "./cmd/planmaxx", "review", "--no-browser", planPath], {
    cwd: root,
    env: {
      ...process.env,
      CODEX_THREAD_ID: "planmaxx-render-thread",
      PATH: `${fakeBinDir}${path.delimiter}${process.env.PATH ?? ""}`,
    },
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
    anchor: { startLine: 14, endLine: 14 },
    body: "Verify audit logs include export requester before finance starts using this.",
  });
  await requestJSON(url, `/api/threads/${encodeURIComponent(auditThread.id)}/reply`, {
    body: "Support also needs row count in the handoff so escalation reports can be traced.",
  });
  await requestJSON(url, "/api/side-questions", {
    threadID: auditThread.id,
    question: "What should we double-check before making this export available to support?",
  });
  await requestJSON(url, "/api/revisions/propose-section", {
    threadId: auditThread.id,
    anchor: { startLine: 14, endLine: 14 },
    instruction: "Clarify the audit-log requirements for this export.",
  });
}

async function writeFakeCodex(binDir) {
  await mkdir(binDir, { recursive: true });
  const binPath = path.join(binDir, "codex");
  await writeFile(binPath, fakeCodexScript(), "utf8");
  await chmod(binPath, 0o755);
}

function fakeCodexScript() {
  return `#!/usr/bin/env node
const readline = require("node:readline");

const sideQuestionAnswer = "Codex says audit fields should be emitted after the export stream finishes, so requester, account ID, and row count stay tied to the same export attempt.";
let forkCounter = 0;
let turnCounter = 0;

function send(message) {
  process.stdout.write(JSON.stringify(message) + "\\n");
}

function answerFor(params) {
  const prompt = params.input?.[0]?.text ?? "";
  const revision = prompt.match(/<planmaxx_iteration\\b[^>]*\\brevision="([^"]+)"/);
  if (!revision) return sideQuestionAnswer;
  return '<planmaxx_proposal version="1" revision="' + revision[1] + '"><summary>Clarified audit logging.</summary><replacement target="lines"><expected>4. Ensure audit logs include export requester, account ID, date range, and row count.</expected><content>4. Ensure audit logs record the export requester, account ID, date range, row count, and retention window.</content></replacement></planmaxx_proposal>';
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on("line", (line) => {
  if (!line.trim()) return;
  const request = JSON.parse(line);
  if (request.id === undefined) return;
  const id = request.id;
  const params = request.params || {};

  if (request.method === "initialize") {
    send({ id, result: { userAgent: "Codex Test", codexHome: "/tmp/codex", platformFamily: "unix", platformOs: "macos" } });
    return;
  }
  if (request.method === "thread/read") {
    send({ id, result: { thread: { id: params.threadId, ephemeral: false, cwd: process.cwd(), status: { type: "idle" } }, cwd: process.cwd() } });
    return;
  }
  if (request.method === "thread/fork") {
    const forkId = "planmaxx-render-fork-" + (++forkCounter);
    send({ id, result: { thread: { id: forkId, forkedFromId: params.threadId, ephemeral: true, cwd: params.cwd || process.cwd(), status: { type: "idle" } }, cwd: params.cwd || process.cwd() } });
    return;
  }
  if (request.method === "turn/start") {
    const turnId = "planmaxx-render-turn-" + (++turnCounter);
    send({ id, result: { turn: { id: turnId, status: "inProgress" } } });
    setTimeout(() => {
      send({ method: "item/completed", params: { threadId: params.threadId, turnId, item: { type: "agentMessage", text: answerFor(params) } } });
      send({ method: "turn/completed", params: { threadId: params.threadId, turn: { id: turnId, status: "completed" } } });
    }, 5);
    return;
  }
  send({ id, error: { code: -32601, message: "method not found" } });
});
`;
}

async function renderScreenshots(chromium, url) {
  const renderTheme = process.env.PLANMAXX_RENDER_THEME === "light" ? "light" : "dark";
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
    const page = await browser.newPage({
      viewport: { width: 1600, height: 1020 },
      deviceScaleFactor: 2,
    });
    await page.addInitScript((theme) => window.localStorage.setItem("planmaxx.theme", theme), renderTheme);
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForSelector(".thread", { timeout: 5_000 });
    await page.waitForSelector(".inline-proposal-controls", { timeout: 5_000 });
    await page.waitForSelector(".review-navigator", { timeout: 5_000 });
    await page.waitForFunction(() => document.querySelector('button[title="Show threads directly below their anchored range"]')?.getAttribute("aria-pressed") === "true");
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.evaluate(() => { document.querySelector("main").style.zoom = "1.08"; });
    await page.waitForTimeout(250);
    await page.screenshot({
      path: path.join(readmeScreenshotDir, "review-desktop.png"),
      fullPage: false,
    });
    await page.evaluate(() => { document.querySelector("main").style.zoom = "1"; });
    await page.waitForTimeout(150);
    await page.getByRole("button", { name: "Discard" }).click();
    await page.locator(".inline-proposal-controls").waitFor({ state: "hidden" });
    await page.getByRole("button", { name: "Finalize" }).click();
    await page.locator('dialog[aria-label="Review approval"]').waitFor({ state: "visible" });
    await page.locator(".modal-card").screenshot({
      path: path.join(readmeScreenshotDir, "handoff-preview.png"),
    });
    await page.keyboard.press("Escape");
    await page.locator('dialog[aria-label="Review approval"]').waitFor({ state: "hidden" });
    const alongsideButton = page.getByRole("button", { name: "Alongside" });
    await alongsideButton.click();
    await page.waitForFunction(() => document.querySelector('button[title="Show threads beside their final anchored line"]')?.getAttribute("aria-pressed") === "true");
    await page.waitForSelector(".plan-comment-rail .thread", { timeout: 5_000 });
    await page.waitForTimeout(250);
    await page.setViewportSize({ width: 1440, height: 1080 });
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.locator('[data-thread-id="thread-1"]').screenshot({
      path: path.join(readmeScreenshotDir, "thread-card.png"),
    });
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
