import { chromium } from "../web/node_modules/playwright/index.mjs";

const url = process.argv[2];
const mode = process.argv[3] ?? "proposal";
if (!url) throw new Error("usage: e2e-browser.mjs <review-url> [proposal|revision|states]");

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
    const revisionTrigger = page.getByRole("button", { name: "Revisions — current rev-2" });
    if (await revisionTrigger.count() !== 1) {
      throw new Error("current revision is not exposed in the top bar");
    }
    await revisionTrigger.click();
    const revisionDialog = page.getByRole("dialog", { name: "Revisions" });
    await revisionDialog.waitFor();
    for (const revision of ["rev-2", "rev-1"]) {
      if (await revisionDialog.getByText(revision, { exact: true }).count() !== 1) {
        throw new Error(`revision dialog is missing ${revision}`);
      }
    }
    await revisionDialog.getByRole("button", { name: "Close", exact: true }).click();
    await page.getByText("Showing changes: rev-1 → rev-2", { exact: false }).waitFor();
	const revisionNavigator = page.getByRole("navigation", { name: "Review comments and changes" });
	await revisionNavigator.getByRole("button", { name: "Next" }).click();
	await revisionNavigator.getByText("1 of 1", { exact: true }).waitFor();
	if (!(await revisionNavigator.getByRole("button", { name: "Next" }).isDisabled())) throw new Error("revision navigation does not stop at the end");
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
    await page.getByRole("button", { name: "Iterate", exact: true }).click();
    const iterateDialog = page.getByRole("dialog", { name: "Review iteration" });
    await iterateDialog.waitFor();
    if (await iterateDialog.getByRole("button", { name: "Create proposal" }).count() !== 1) throw new Error("iteration quick review is missing its primary action");
    if (await iterateDialog.getByText("comparison live comment", { exact: true }).count() !== 1) throw new Error("iteration digest content was missing or duplicated");
    await iterateDialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.getByRole("button", { name: "Finalize", exact: true }).click();
    const finalizeDialog = page.getByRole("dialog", { name: "Review approval" });
    await finalizeDialog.waitFor();
    if (await finalizeDialog.getByRole("button", { name: "Approve and submit" }).count() !== 1) throw new Error("approval quick review is missing its primary action");
    if (await finalizeDialog.getByText("comparison live comment", { exact: true }).count() !== 1) throw new Error("approval digest content was missing or duplicated");
    await finalizeDialog.getByRole("button", { name: "Cancel", exact: true }).click();
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode === "states") {
    await page.getByText("active instruction", { exact: true }).waitFor();
    await page.getByText("active private", { exact: true }).waitFor();
    const navigator = page.getByRole("navigation", { name: "Review comments and changes" });
    await navigator.waitFor();
    const attentionSummary = page.getByText("1 unanchored comment", { exact: false });
    await attentionSummary.waitFor();
    const navigatorBeforeAttention = await page.evaluate(() => {
      const nav = document.querySelector(".review-navigator");
      const attention = document.querySelector(".attention-overview");
      return Boolean(nav && attention && (nav.compareDocumentPosition(attention) & Node.DOCUMENT_POSITION_FOLLOWING));
    });
    if (!navigatorBeforeAttention) throw new Error("review navigator is hidden behind unanchored feedback");
    const detachedFeedback = page.getByText("detached feedback", { exact: true });
    if (await detachedFeedback.isVisible()) throw new Error("unanchored feedback did not start collapsed");
    await attentionSummary.click();
    await detachedFeedback.waitFor();
    await page.getByRole("button", { name: "Mark addressed…" }).click();
    const addressDialog = page.getByRole("dialog", { name: "Record feedback as addressed" });
    await addressDialog.waitFor();
    if (await addressDialog.getByText("rev-2 · External source change · suggested", { exact: true }).count() !== 1) throw new Error("external revision was not suggested");
    await addressDialog.getByRole("button", { name: "Record as addressed" }).click();
    await page.getByText("Feedback recorded for this revision", { exact: true }).waitFor();
    if (await page.getByText("1 unanchored comment", { exact: false }).count() !== 0) throw new Error("addressed feedback remained in attention");
    await page.getByRole("button", { name: "Hide changes" }).click();
    const history = page.getByText("Show addressed feedback (2)", { exact: true });
    await history.click();
    await page.getByText("addressed feedback", { exact: true }).waitFor();
    await page.getByText("detached feedback", { exact: true }).waitFor();
    if (await page.getByRole("button", { name: "Use in iteration", exact: true }).count() !== 2) throw new Error("active intent controls are not scoped to active feedback");
    if (await page.getByRole("button", { name: "Create follow-up" }).count() !== 2) throw new Error("addressed feedback is missing follow-up action");
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode !== "proposal") {
    throw new Error(`unknown browser E2E mode: ${mode}`);
  } else {
  await page.getByText("Pending whole-plan iteration", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Apply as new revision" }).waitFor();
  if (!(await page.getByRole("button", { name: "Iterate", exact: true }).isDisabled()) || !(await page.getByRole("button", { name: "Finalize", exact: true }).isDisabled())) {
    throw new Error("submission actions are enabled while a proposal is pending");
  }

  const navigator = page.getByRole("navigation", { name: "Review comments and changes" });
  await navigator.waitFor();
  if (await navigator.getByText("5 to review", { exact: true }).count() !== 1) throw new Error("proposal review queue count is incorrect");
  await page.getByPlaceholder("Filter comments").fill("no matching comment");
  await navigator.getByRole("button", { name: "Next" }).click();
  await navigator.getByText("1 of 5", { exact: true }).waitFor();
  if (await page.getByText("replace both lines", { exact: true }).count() !== 1) throw new Error("navigation did not reveal a filtered comment");
  for (let step = 2; step <= 5; step++) {
    await navigator.getByRole("button", { name: "Next" }).click();
    await navigator.getByText(`${step} of 5`, { exact: true }).waitFor();
  }
  if (!(await navigator.getByRole("button", { name: "Next" }).isDisabled())) throw new Error("review navigation wraps past the final stop");
  await navigator.getByRole("button", { name: "Previous" }).click();
  await navigator.getByText("4 of 5", { exact: true }).waitFor();
  await page.getByPlaceholder("Filter comments").fill("");

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
  const alongsideLayout = await page.evaluate(() => {
    const main = document.querySelector("main");
    const article = document.querySelector(".plan-markdown");
    const rail = document.querySelector(".plan-comment-rail");
    if (!main || !article || !rail) return null;
    const mainRect = main.getBoundingClientRect();
    const articleRect = article.getBoundingClientRect();
    const railRect = rail.getBoundingClientRect();
    return {
      articleWidth: articleRect.width,
      railAfterArticle: railRect.left >= articleRect.right,
      railAtMainEdge: Math.abs(mainRect.right - railRect.right) <= 20,
      revisionsInPage: document.querySelectorAll(".revision-panel").length,
    };
  });
  if (!alongsideLayout || alongsideLayout.articleWidth < 700) {
    throw new Error("alongside comments still consume a nested third column from the plan");
  }
  if (!alongsideLayout.railAfterArticle || !alongsideLayout.railAtMainEdge) {
    throw new Error("alongside comments are not using the page sidebar");
  }
  if (alongsideLayout.revisionsInPage !== 0) {
    throw new Error("revision rail remained in the page after moving revisions to the top bar");
  }
  if (await page.getByText("overlapping replacement", { exact: true }).count() !== 1) {
    throw new Error("overlapping comment duplicated after switching layouts");
  }
  if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  }
} finally {
  await browser.close();
}
