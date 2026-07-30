import { chromium } from "../web/node_modules/playwright/index.mjs";

const url = process.argv[2];
const mode = process.argv[3] ?? "proposal";
if (!url) throw new Error("usage: e2e-browser.mjs <review-url> [proposal|revision|states|html|markdown-workflows]");

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ colorScheme: "dark" });
const consoleErrors = [];
page.on("console", (message) => {
  if (message.type() === "error") consoleErrors.push(message.text());
});
page.on("pageerror", (error) => consoleErrors.push(error.message));

async function previewHoverState(page, locator) {
  await page.evaluate(() => {
    window.__planmaxxHoverEvents = [];
  });
  await locator.hover();
  await page.waitForFunction(() => window.__planmaxxHoverEvents.length > 0);
  return page.evaluate(() => window.__planmaxxHoverEvents.at(-1));
}

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
	await revisionNavigator.getByRole("button", { name: "Next review item" }).click();
	await revisionNavigator.getByText("1 / 2", { exact: true }).waitFor();
	await revisionNavigator.getByRole("button", { name: "Next review item" }).click();
	await revisionNavigator.getByText("2 / 2", { exact: true }).waitFor();
	if (!(await revisionNavigator.getByRole("button", { name: "Next review item" }).isDisabled())) throw new Error("revision navigation does not stop at the end");
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
    const navigatorFloatsAboveAttention = await page.evaluate(() => {
      const nav = document.querySelector(".review-navigator");
      const attention = document.querySelector(".attention-overview");
      if (!nav || !attention) return false;
      const style = getComputedStyle(nav);
      return style.position === "fixed" && Number(style.zIndex) > (Number.parseInt(getComputedStyle(attention).zIndex, 10) || 0);
    });
    if (!navigatorFloatsAboveAttention) throw new Error("review navigator is hidden behind unanchored feedback");
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
  } else if (mode === "html") {
    const preview = page.frameLocator(".html-plan-preview");
    await preview.getByRole("heading", { name: "Launch & rollback plan" }).waitFor();
    await preview.getByRole("table", { name: "Rollout gates" }).waitFor();
    await preview.getByRole("img", { name: "Rollout flow" }).waitFor();
    await preview.getByText("planmaxx review launch.html", { exact: true }).waitFor();
    await page.evaluate(() => {
      window.__planmaxxHoverEvents = [];
      window.addEventListener("message", (event) => {
        if (event.data?.type === "planmaxx:preview-hover") window.__planmaxxHoverEvents.push(event.data);
      });
    });
    await page.getByText("Existing list comment", { exact: true }).waitFor();
    await page.getByText("Existing table comment", { exact: true }).waitFor();
    await page.getByText("Existing heading element comment", { exact: true }).waitFor();

    const existingHeadingHover = await previewHoverState(page, preview.getByRole("heading", { name: "Safety checks" }));
    if (existingHeadingHover.tagName !== "h2" || existingHeadingHover.state !== "existing" || existingHeadingHover.rectCount < 1) {
      throw new Error(`existing element hover target is incorrect: ${JSON.stringify(existingHeadingHover)}`);
    }

    const tableHover = await previewHoverState(page, preview.getByText("Required", { exact: true }));
    if (tableHover.tagName !== "td" || tableHover.state !== "new" || tableHover.rectCount < 1) {
      throw new Error(`table-cell hover target is incorrect: ${JSON.stringify(tableHover)}`);
    }
    const svgPathHover = await previewHoverState(page, preview.locator("path"));
    if (svgPathHover.tagName !== "path" || svgPathHover.rectCount < 1) {
      throw new Error(`SVG path hover target is incorrect: ${JSON.stringify(svgPathHover)}`);
    }
    const summary = preview.getByText("Fallback", { exact: true });
    const summaryHover = await previewHoverState(page, summary);
    if (summaryHover.tagName !== "summary" || summaryHover.rectCount < 1) {
      throw new Error(`details summary hover target is incorrect: ${JSON.stringify(summaryHover)}`);
    }
    await summary.click();
    if (await preview.locator("details").getAttribute("open") === null) {
      throw new Error("HTML element targeting prevented native details toggling");
    }
    if (await page.getByRole("complementary", { name: "HTML preview comment" }).count() !== 0) {
      throw new Error("native details toggling opened an element-comment composer");
    }

    await page.evaluate(() => {
      window.__planmaxxPreviewPositions = [];
      window.addEventListener("message", (event) => {
        if (event.data?.type === "planmaxx:preview-position") window.__planmaxxPreviewPositions.push(event.data);
      });
    });
    await preview.getByText("Verify the final checkpoint.", { exact: true }).evaluate((element) => element.scrollIntoView());
    await page.waitForTimeout(100);
    const multilineHover = await previewHoverState(
      page,
      preview.getByText("the deliberately long inline planning constraint that wraps across multiple rendered lines before approval", { exact: true }),
    );
    if (multilineHover.tagName !== "span" || multilineHover.rectCount < 2) {
      throw new Error(`multiline inline hover did not use segmented borders: ${JSON.stringify(multilineHover)}`);
    }
    await preview.getByText("Verify the final checkpoint.", { exact: true }).evaluate((element) => element.scrollIntoView());
    await page.waitForTimeout(100);
    const previewPosition = await page.evaluate(() => window.__planmaxxPreviewPositions.at(-1));
    if (!previewPosition?.line || previewPosition.line <= 1) throw new Error(`Preview did not publish its scrolled source position: ${JSON.stringify(previewPosition)}`);
    await page.getByRole("button", { name: "Source" }).click();
    const appendixSourceRow = page.locator(`[data-document-line="${previewPosition.line}"]`);
    await appendixSourceRow.waitFor();
    await page.waitForTimeout(100);
    const restoredSourcePosition = await appendixSourceRow.evaluate((row) => ({
      top: row.getBoundingClientRect().top,
      bottom: row.getBoundingClientRect().bottom,
      viewport: window.innerHeight,
      scrollY: window.scrollY,
    }));
    if (
      restoredSourcePosition.scrollY <= 0 ||
      restoredSourcePosition.bottom <= 60 ||
      restoredSourcePosition.top >= restoredSourcePosition.viewport
    ) {
      throw new Error(`Preview scroll position was not restored in Source: ${JSON.stringify(restoredSourcePosition)} preview=${JSON.stringify(previewPosition)}`);
    }
    await page.waitForFunction(() => document.querySelectorAll('[data-document-line="2"] .line-content span[style*="color"]').length > 1);
    if (await page.locator('[data-document-line="2"] .line-content').getAttribute("data-source-indent") !== "1") {
      throw new Error("HTML Source did not apply structural indentation");
    }
    if (await page.locator('[data-document-line="5"] .line-content').getAttribute("data-source-indent") !== "2") {
      throw new Error("nested HTML Source indentation is incorrect");
    }

    await page.locator('[data-document-line="28"]').evaluate((row) => {
      window.scrollTo({ top: window.scrollY + row.getBoundingClientRect().top - 72, behavior: "auto" });
    });
    await page.waitForTimeout(100);
    const sourceScrollState = await page.locator('[data-document-line="28"]').evaluate((row) => ({
      top: row.getBoundingClientRect().top,
      scrollY: window.scrollY,
      maxScroll: document.documentElement.scrollHeight - window.innerHeight,
      viewport: window.innerHeight,
      articleScrollTop: row.closest("article")?.scrollTop,
    }));
    if (sourceScrollState.scrollY <= 0 || sourceScrollState.top >= sourceScrollState.viewport) {
      throw new Error(`Source test could not establish a scrolled position: ${JSON.stringify(sourceScrollState)}`);
    }
    await page.getByRole("button", { name: "Preview" }).click();
    const restoredPreviewTarget = preview.getByText("Verify alert routing.", { exact: true });
    await restoredPreviewTarget.waitFor();
    await page.waitForTimeout(100);
    const restoredPreviewPosition = await restoredPreviewTarget.evaluate((target) => ({
      top: target.getBoundingClientRect().top,
      bottom: target.getBoundingClientRect().bottom,
      height: window.innerHeight,
      scrollY: window.scrollY,
    }));
    if (
      restoredPreviewPosition.scrollY <= 0 ||
      restoredPreviewPosition.bottom <= 0 ||
      restoredPreviewPosition.top >= restoredPreviewPosition.height
    ) {
      const positions = await page.evaluate(() => window.__planmaxxPreviewPositions);
      throw new Error(`Source scroll position was not restored in Preview: ${JSON.stringify(restoredPreviewPosition)} source=${JSON.stringify(sourceScrollState)} positions=${JSON.stringify(positions)}`);
    }

    await preview.getByRole("heading", { name: "Safety checks" }).click();
    await page.locator(".thread.is-focus").filter({ hasText: "Existing heading element comment" }).waitFor();
    if (await page.getByRole("complementary", { name: "HTML preview comment" }).count() !== 0) {
      throw new Error("clicking an already-commented HTML element opened a duplicate composer");
    }

    const previewListItem = preview.getByText("Ship & iterate carefully.", { exact: true });
    const listHover = await previewHoverState(page, previewListItem);
    if (listHover.tagName !== "li" || listHover.state !== "new" || listHover.rectCount < 1) {
      throw new Error(`list-item hover target is incorrect: ${JSON.stringify(listHover)}`);
    }
    await previewListItem.click();
    const elementComposer = page.getByRole("complementary", { name: "HTML preview comment" });
    await elementComposer.waitFor();
    if (!((await elementComposer.innerText()).includes("Line 8"))) {
      throw new Error("a nested range comment hijacked the full list-element target");
    }
    await elementComposer.getByRole("button", { name: "Cancel" }).click();
    await previewListItem.evaluate((item) => {
      const text = [...item.childNodes].find((node) => node.nodeType === Node.TEXT_NODE);
      if (!text) throw new Error("HTML preview paragraph has no text node");
      const range = document.createRange();
      range.setStart(text, 7);
      range.setEnd(text, 14);
      const selection = document.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    });
    const previewComposer = page.getByRole("complementary", { name: "HTML preview comment" });
    await previewComposer.waitFor({ timeout: 5_000 }).catch(async (error) => {
      const frameState = await preview.locator("body").evaluate(() => {
        const comments = [];
        const walker = document.createTreeWalker(document, NodeFilter.SHOW_COMMENT);
        let comment;
        while ((comment = walker.nextNode())) comments.push(comment.data);
        return {
          bridgeConfigured: Boolean(window.__PLANMAXX_PREVIEW__),
          comments,
          selection: document.getSelection()?.toString() ?? "",
        };
      });
      throw new Error(`HTML preview selection did not open the composer: ${error}\nframe=${JSON.stringify(frameState)}\nconsole=${consoleErrors.join("\n")}`);
    });
    const selectionHoverState = await page.evaluate(() => window.__planmaxxHoverEvents.at(-1));
    if (selectionHoverState?.state !== "none") {
      throw new Error(`text range selection did not suppress the element hover target: ${JSON.stringify(selectionHoverState)}`);
    }
    await previewComposer.getByPlaceholder("Leave a comment for this selection...").fill("Can this preserve rollback?");
    await previewComposer.getByRole("button", { name: "/btw", exact: true }).click();
    await page.getByText("Keep the rollback owner explicit and verify the fallback path before rollout.", { exact: true }).waitFor();
    const includeAnswer = page.getByRole("button", { name: "Include answer", exact: true });
    await includeAnswer.click();
    await page.getByText("/btw answer will be used for iteration or approval", { exact: true }).waitFor();
    await preview.locator("body").evaluate(() => document.getSelection()?.removeAllRanges());

    await preview.getByText("Error budget", { exact: true }).first().click();
    await page.getByRole("complementary", { name: "HTML preview comment" }).waitFor();
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByRole("button", { name: "Cancel" }).click();

    await preview.getByText("Deploy", { exact: true }).click();
    await page.getByRole("complementary", { name: "HTML preview comment" }).waitFor();
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByRole("button", { name: "Cancel" }).click();

    await page.getByRole("button", { name: "Alongside" }).click();
    await page.locator(".html-preview-comment-rail").getByText("Existing list comment", { exact: true }).waitFor();
    const alongsideHover = await previewHoverState(page, preview.getByText("Required", { exact: true }));
    if (alongsideHover.tagName !== "td" || alongsideHover.rectCount < 1) {
      throw new Error(`HTML hover targeting failed with alongside comments: ${JSON.stringify(alongsideHover)}`);
    }
    const htmlRailPosition = await page.evaluate(() => {
      const frame = document.querySelector(".html-plan-preview");
      const rail = document.querySelector(".html-preview-comment-rail");
      if (!frame || !rail) return null;
      const frameRect = frame.getBoundingClientRect();
      const railRect = rail.getBoundingClientRect();
      return { separate: railRect.left >= frameRect.right, aligned: railRect.top <= frameRect.top && frameRect.top - railRect.top < 120 };
    });
    if (!htmlRailPosition?.separate || !htmlRailPosition.aligned) throw new Error("HTML alongside comments are not positioned beside the rendered plan");
    await page.getByRole("button", { name: "In place" }).click();

    await page.setViewportSize({ width: 390, height: 844 });
    const mobileHover = await previewHoverState(page, preview.getByText("Ship & iterate carefully.", { exact: true }));
    if (mobileHover.tagName !== "li" || mobileHover.rectCount < 1) {
      throw new Error(`HTML hover targeting failed at the mobile breakpoint: ${JSON.stringify(mobileHover)}`);
    }
    await page.setViewportSize({ width: 1280, height: 720 });

    await page.getByTitle("Browse document sections").click();
    const outline = page.getByRole("navigation", { name: "Document sections" });
    for (const section of [
      "Launch & rollback plan · line 2",
      "Rollout controls · line 4",
      "Safety checks · line 5",
    ]) {
      await outline.getByTitle(section).click();
      if (await page.getByRole("button", { name: "Preview" }).getAttribute("aria-pressed") !== "true") {
        throw new Error(`HTML outline selection left preview mode: ${section}`);
      }
      if (await page.locator(".html-plan-preview").count() !== 1 || await page.locator(".line-row").count() !== 0) {
        throw new Error(`HTML outline selection rendered source rows instead of Preview: ${section}`);
      }
    }

    await previewListItem.evaluate((item) => {
      const text = [...item.childNodes].find((node) => node.nodeType === Node.TEXT_NODE);
      const range = document.createRange();
      range.setStart(text, 7);
      range.setEnd(text, 14);
      const selection = document.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    });
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByPlaceholder("Leave a comment for this selection...").fill("Make this safer");
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByRole("button", { name: "Iterate section" }).click();
    await page.getByText("Pending proposal", { exact: true }).waitFor();
    if (await page.getByRole("button", { name: "Source" }).getAttribute("aria-pressed") !== "true") throw new Error("HTML proposal did not switch to source comparison");
    await page.getByPlaceholder("Ask for a narrower, clearer, or more specific version...").fill("Mention rollback");
    await page.getByRole("button", { name: "Iterate again" }).click();
    await page.getByText("Added rollback emphasis.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Apply as new revision" }).click();
    await page.getByText("Showing changes: rev-1 → rev-2", { exact: false }).waitFor();
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode === "markdown-workflows") {
    await page.getByText("Existing nested-list comment", { exact: true }).waitFor();
    await page.getByText("Existing table-row comment", { exact: true }).waitFor();
    if (await page.locator(".line-row.is-table-row").count() < 2) throw new Error("Markdown table did not render as table rows");
    if (await page.getByText("flowchart LR", { exact: false }).count() !== 1) throw new Error("Markdown diagram code block disappeared");

    const rolloutLine = page.locator('[data-document-line="8"] .line-content');
    await rolloutLine.evaluate((line) => {
      const walker = document.createTreeWalker(line, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const start = node.data.indexOf("Ship carefully");
        if (start < 0) continue;
        const range = document.createRange();
        range.setStart(node, start);
        range.setEnd(node, start + "Ship carefully".length);
        const selection = document.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        line.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
        return;
      }
      throw new Error("could not select Markdown rollout text");
    });
    const composer = page.getByPlaceholder("Leave a comment for this selection...");
    await composer.fill("What is the safest rollout?");
    await page.getByRole("button", { name: "/btw", exact: true }).click();
    await page.getByText("Keep the rollback owner explicit and verify the fallback path before rollout.", { exact: true }).waitFor({ timeout: 5_000 }).catch(async (error) => {
      throw new Error(`Markdown /btw answer did not render: ${error}\npage=${(await page.locator("body").innerText()).slice(-3000)}`);
    });

    await page.getByRole("button", { name: "Ask /btw" }).first().click();
    const askDialog = page.getByRole("dialog", { name: "Ask the agent a side question" });
    await askDialog.getByLabel("Question").fill("Any other risk?");
    await askDialog.getByRole("button", { name: "Ask /btw" }).click();
    await page.waitForFunction((answer) => [...document.querySelectorAll("p")].filter((node) => node.textContent === answer).length === 2,
      "Keep the rollback owner explicit and verify the fallback path before rollout.");

    await rolloutLine.evaluate((line) => {
      const walker = document.createTreeWalker(line, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const start = node.data.indexOf("Ship carefully");
        if (start < 0) continue;
        const range = document.createRange();
        range.setStart(node, start);
        range.setEnd(node, start + "Ship carefully".length);
        const selection = document.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        line.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
        return;
      }
    });
    await page.getByPlaceholder("Leave a comment for this selection...").fill("Use a canary");
    await page.getByRole("button", { name: "Iterate section" }).click();
    await page.getByText("Pending proposal", { exact: true }).waitFor();
    await page.getByPlaceholder("Ask for a narrower, clearer, or more specific version...").fill("Add a rollback gate");
    await page.getByRole("button", { name: "Iterate again" }).click();
    await page.getByText("Added rollback wording.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Apply as new revision" }).click();
    await page.getByText("Showing changes: rev-1 → rev-2", { exact: false }).waitFor();

    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("button", { name: "Alongside" }).click();
    const mobileRail = page.locator(".plan-comment-rail");
    await mobileRail.waitFor();
    const mobileLayout = await mobileRail.evaluate((rail) => {
      const rect = rail.getBoundingClientRect();
      return { left: rect.left, right: rect.right, viewport: window.innerWidth, visible: rect.width > 0 };
    });
    if (!mobileLayout.visible || mobileLayout.left < 0 || mobileLayout.right > mobileLayout.viewport + 1) throw new Error("mobile alongside comments overflow the viewport");
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode !== "proposal") {
    throw new Error(`unknown browser E2E mode: ${mode}`);
  } else {
  await page.getByText("Pending whole-plan iteration", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Apply as new revision" }).waitFor();
  if (!(await page.getByRole("button", { name: "Iterate", exact: true }).isDisabled()) || !(await page.getByRole("button", { name: "Finalize", exact: true }).isDisabled())) {
    throw new Error("submission actions are enabled while a proposal is pending");
  }

  const outlineTrigger = page.getByTitle("Browse document sections");
  await outlineTrigger.click();
  const outline = page.getByRole("navigation", { name: "Document sections" });
  await outline.getByTitle("Regression fixture · line 1").click();
  if (await page.locator('[data-document-line="1"]').count() !== 1) throw new Error("document outline did not map its heading to the current diff row");

  const navigator = page.getByRole("navigation", { name: "Review comments and changes" });
  await navigator.waitFor();
  if (await navigator.getByText("0 / 6", { exact: true }).count() !== 1) throw new Error("proposal review queue count is incorrect");
  await page.getByPlaceholder("Filter comments").fill("no matching comment");
  await navigator.getByRole("button", { name: "Next review item" }).click();
  await navigator.getByText("1 / 6", { exact: true }).waitFor();
  if (await page.getByText("replace both lines", { exact: true }).count() !== 1) throw new Error("navigation did not reveal a filtered comment");
  for (let step = 2; step <= 6; step++) {
    await navigator.getByRole("button", { name: "Next review item" }).click();
    await navigator.getByText(`${step} / 6`, { exact: true }).waitFor();
  }
  if (!(await navigator.getByRole("button", { name: "Next review item" }).isDisabled())) throw new Error("review navigation wraps past the final stop");
  await navigator.getByRole("button", { name: "Previous review item" }).click();
  await navigator.getByText("5 / 6", { exact: true }).waitFor();
  await page.keyboard.press("Alt+ArrowDown");
  await navigator.getByText("6 / 6", { exact: true }).waitFor();
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
