import { chromium } from "../web/node_modules/playwright/index.mjs";

const url = process.argv[2];
const mode = process.argv[3] ?? "proposal";
if (!url) throw new Error("usage: e2e-browser.mjs <review-url> [proposal|revision]");

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ colorScheme: "dark" });
const consoleErrors = [];
page.on("console", (message) => {
  if (message.type() === "error") consoleErrors.push(message.text());
});
page.on("pageerror", (error) => consoleErrors.push(error.message));

try {
  await page.goto(url, { waitUntil: "networkidle" });
  if (mode === "revision") {
    await page.getByText("Showing changes: rev-1 → rev-2", { exact: false }).waitFor();
    const feedback = page.getByText("revision placement comment", { exact: true });
    if (await feedback.count() !== 1) {
      throw new Error("accepted revision feedback was missing or duplicated");
    }
    const feedbackOrderIsCorrect = await feedback.evaluate((message) => {
      const card = message.closest(".comparison-feedback-card");
      const placement = card?.closest(".plan-row-with-comments");
      const placedRow = placement?.querySelector(".line-row");
      const clusterId = placedRow?.getAttribute("data-change-cluster");
      const changedRows = clusterId ? [...document.querySelectorAll(`[data-change-cluster="${clusterId}"]`)] : [];
      const finalChangedRow = changedRows.at(-1);
      return Boolean(finalChangedRow && card && (finalChangedRow.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING));
    });
    if (!feedbackOrderIsCorrect) throw new Error("revision feedback rendered before the complete change cluster");
	await page.getByRole("button", { name: "Comment on line 1" }).click();
	await page.getByPlaceholder("Leave a comment for this selection...").fill("comparison live comment");
	await page.getByRole("button", { name: "Add comment" }).click();
	await page.getByText("comparison live comment", { exact: true }).waitFor();
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode !== "proposal") {
    throw new Error(`unknown browser E2E mode: ${mode}`);
  } else {
  await page.getByText("Pending whole-plan iteration", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Apply as new revision" }).waitFor();

  const addedTableRows = page.locator(".line-row.is-proposal-add.is-table-row");
  if (await addedTableRows.count() < 2) {
    throw new Error("added Markdown table rows were not rendered as table rows");
  }

  for (const comment of ["replace both lines", "overlapping replacement"]) {
    if (await page.getByText(comment, { exact: true }).count() !== 1) {
      throw new Error(`comment was missing or duplicated: ${comment}`);
    }
  }

  const inlineOrderIsCorrect = await page.evaluate(() => {
	const comments = [...document.querySelectorAll(".plan-thread-stack.is-inline")];
	const target = comments.find((element) => element.textContent?.includes("replace both lines"));
	const placement = target?.closest(".plan-row-with-comments");
	const placedRow = placement?.querySelector(".line-row");
	const clusterId = placedRow?.getAttribute("data-change-cluster");
	const changedRows = clusterId ? [...document.querySelectorAll(`[data-change-cluster="${clusterId}"]`)] : [];
	const finalChangedRow = changedRows.at(-1);
	return Boolean(finalChangedRow && target && (finalChangedRow.compareDocumentPosition(target) & Node.DOCUMENT_POSITION_FOLLOWING));
  });
  if (!inlineOrderIsCorrect) throw new Error("comment rendered before the complete remove/add cluster");

  await page.getByRole("button", { name: "Alongside" }).click();
  await page.locator(".plan-thread-stack.is-alongside").filter({ hasText: "replace both lines" }).waitFor();
  if (await page.getByText("overlapping replacement", { exact: true }).count() !== 1) {
    throw new Error("overlapping comment duplicated after switching layouts");
  }
  if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  }
} finally {
  await browser.close();
}
