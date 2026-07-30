import { chromium } from "../web/node_modules/playwright/index.mjs";

const url = process.argv[2];
const mode = process.argv[3] ?? "proposal";
if (!url) throw new Error("usage: e2e-browser.mjs <review-url> [standalone|proposal|revision|states|html|markdown-workflows]");

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
  if (mode === "standalone") {
    await page.getByRole("banner").getByText("Manual review", { exact: true }).waitFor();
    await page.getByText("Assisted actions off", { exact: true }).waitFor();
    if (!(await page.getByRole("button", { name: "Iterate", exact: true }).isDisabled())) {
      throw new Error("whole-plan iteration is enabled without an attached agent");
    }

    const refine = page.getByLabel("Refine", { exact: true });
    await refine.waitFor();
    if (!(await refine.isDisabled())) throw new Error("proposal refinement is enabled without an attached agent");
    if (!(await page.getByRole("button", { name: "Iterate again" }).isDisabled())) {
      throw new Error("proposal re-iteration is enabled without an attached agent");
    }
    if (await page.getByRole("button", { name: "Discard" }).isDisabled()) {
      throw new Error("local proposal discard is disabled in standalone mode");
    }
    if (await page.getByRole("button", { name: "Apply as new revision" }).isDisabled()) {
      throw new Error("local proposal apply is disabled in standalone mode");
    }
    await page.getByRole("button", { name: "Discard" }).click();
    await refine.waitFor({ state: "detached" });

    await page.getByRole("button", { name: "Comment on line 3" }).click();
    await page.getByPlaceholder("Leave a comment for this selection...").fill("Manual review comment");
    if (await page.getByRole("button", { name: "/btw", exact: true }).count() !== 0) {
      throw new Error("inline /btw action is visible without an attached agent");
    }
    if (await page.getByRole("button", { name: "Iterate section", exact: true }).count() !== 0) {
      throw new Error("inline section iteration is visible without an attached agent");
    }
    await page.getByRole("button", { name: "Add comment" }).click();
    await page.getByText("Manual review comment", { exact: true }).waitFor();
    if (await page.getByRole("button", { name: "Ask /btw" }).count() !== 0) {
      throw new Error("thread /btw action is visible without an attached agent");
    }
    if (await page.getByRole("button", { name: "Iterate now" }).count() !== 0) {
      throw new Error("thread iteration is visible without an attached agent");
    }

    await page.getByRole("button", { name: "Finalize", exact: true }).click();
    const finalizeDialog = page.getByRole("dialog", { name: "Review approval" });
    await finalizeDialog.waitFor();
    await finalizeDialog.getByRole("button", { name: "Approve and submit" }).click();
    await page.getByText("Plan saved", { exact: true }).waitFor();
    await page.getByText("review handoff is being returned in the terminal", { exact: false }).waitFor();
    if (await page.getByText("calling agent", { exact: false }).count() !== 0) {
      throw new Error("standalone completion still refers to a calling agent");
    }
    if (consoleErrors.length) throw new Error(`browser console errors:\n${consoleErrors.join("\n")}`);
  } else if (mode === "revision") {
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
      window.__planmaxxPreviewLayouts = [];
      window.__planmaxxFocusEvents = [];
      window.addEventListener("message", (event) => {
        if (event.data?.type === "planmaxx:preview-hover") window.__planmaxxHoverEvents.push(event.data);
        if (event.data?.type === "planmaxx:preview-layout") window.__planmaxxPreviewLayouts.push(event.data);
        if (event.data?.type === "planmaxx:preview-focus-thread") window.__planmaxxFocusEvents.push(event.data);
      });
    });
    await page.getByText("Existing list comment", { exact: true }).waitFor({ state: "attached" });
    await page.getByText("Existing table comment", { exact: true }).waitFor({ state: "attached" });
    await page.getByText("Existing heading element comment", { exact: true }).waitFor({ state: "attached" });
    await page.evaluate(() => {
      window.__planmaxxPreviewPositions = [];
      window.addEventListener("message", (event) => {
        if (event.data?.type === "planmaxx:preview-position") window.__planmaxxPreviewPositions.push(event.data);
      });
    });

    await preview.locator("body").evaluate(() => window.scrollTo({ top: 180, behavior: "auto" }));
    await page.waitForFunction(() => window.__planmaxxPreviewPositions.length > 0);
    const shallowPreviewPosition = await page.evaluate(() => window.__planmaxxPreviewPositions.at(-1));
    if (!shallowPreviewPosition?.line) {
      throw new Error(`Preview did not publish its first visible rendered element: ${JSON.stringify(shallowPreviewPosition)}`);
    }
    await page.getByRole("button", { name: "Source" }).click();
    const shallowSourceRow = page.locator(`[data-document-line="${shallowPreviewPosition.line}"]`);
    await shallowSourceRow.waitFor();
    await page.waitForTimeout(100);
    const shallowSourcePosition = await shallowSourceRow.evaluate((row) => ({
      top: row.getBoundingClientRect().top,
      bottom: row.getBoundingClientRect().bottom,
      viewport: window.innerHeight,
      scrollY: window.scrollY,
      text: row.textContent,
    }));
    if (
      shallowSourcePosition.bottom <= 60 ||
      shallowSourcePosition.top >= shallowSourcePosition.viewport ||
      shallowSourcePosition.text?.includes("planmaxx-scroll-filler")
    ) {
      throw new Error(
        `A shallow Preview scroll restored Source inside non-rendered head content: ${JSON.stringify(shallowSourcePosition)} preview=${JSON.stringify(shallowPreviewPosition)}`,
      );
    }
    await page.getByRole("button", { name: "Preview" }).click();
    await preview.getByRole("heading", { name: "Launch & rollback plan" }).waitFor();

    const headingTarget = preview.getByRole("heading", { name: "Safety checks" });
    await headingTarget.evaluate((element) => element.scrollIntoView({ block: "center" }));
    const inlineHeadingCard = page.locator(".html-preview-anchored-overlay").filter({ hasText: "Existing heading element comment" });
    await inlineHeadingCard.waitFor();
    const inlinePlacement = await Promise.all([
      headingTarget.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom };
      }),
      inlineHeadingCard.evaluate((card) => {
        const rect = card.getBoundingClientRect();
        const shell = card.closest(".html-preview-frame-shell")?.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom, shellTop: shell?.top, shellBottom: shell?.bottom };
      }),
      page.locator(".html-plan-preview").evaluate((frame) => {
        const rect = frame.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom };
      }),
    ]);
    const [headingRect, inlineCardRect, inlineFrameRect] = inlinePlacement;
    if (
      inlineCardRect.shellTop === undefined ||
      inlineCardRect.shellBottom === undefined ||
      inlineCardRect.top < inlineFrameRect.top + headingRect.bottom - 3 ||
      inlineCardRect.top < inlineCardRect.shellTop ||
      inlineCardRect.bottom > inlineCardRect.shellBottom + 1
    ) {
      throw new Error(`HTML inline comment is not inside the iframe at its target: ${JSON.stringify(inlinePlacement)}`);
    }
    const outerOverflow = await page.evaluate(() => ({
      viewportWidth: window.innerWidth,
      documentWidth: document.documentElement.scrollWidth,
      staticPreviewList: document.querySelectorAll(".html-preview-comments").length,
    }));
    if (outerOverflow.documentWidth > outerOverflow.viewportWidth + 1 || outerOverflow.staticPreviewList !== 0) {
      throw new Error(`HTML inline comments overflow or still use a static list: ${JSON.stringify(outerOverflow)}`);
    }
    await inlineHeadingCard.hover();
    const wheelBefore = await Promise.all([
      preview.locator("body").evaluate(() => window.scrollY),
      page.evaluate(() => window.scrollY),
    ]);
    await page.mouse.wheel(0, 120);
    await page.waitForFunction((previous) => {
      const frame = document.querySelector(".html-plan-preview");
      return Boolean(frame && previous >= 0);
    }, wheelBefore[0]);
    await page.waitForTimeout(100);
    const wheelAfter = await Promise.all([
      preview.locator("body").evaluate(() => window.scrollY),
      page.evaluate(() => window.scrollY),
    ]);
    if (wheelAfter[0] <= wheelBefore[0] || Math.abs(wheelAfter[1] - wheelBefore[1]) > 1) {
      throw new Error(`Wheel over an inline HTML comment escaped to the page: before=${JSON.stringify(wheelBefore)} after=${JSON.stringify(wheelAfter)}`);
    }
    await headingTarget.evaluate((element) => element.scrollIntoView({ block: "center" }));

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
    const headSourceRow = page.locator("[data-document-line]").filter({ hasText: "<head>" }).first();
    const mainSourceRow = page.locator("[data-document-line]").filter({ hasText: "<main>" }).first();
    await page.waitForFunction(() =>
      [...document.querySelectorAll("[data-document-line]")].some(
        (row) => row.textContent?.includes("<main>") && row.querySelectorAll('.line-content span[style*="color"]').length > 1,
      ),
    );
    if (await headSourceRow.locator(".line-content").getAttribute("data-source-indent") !== "1") {
      throw new Error("HTML Source did not apply structural indentation");
    }
    if (await mainSourceRow.locator(".line-content").getAttribute("data-source-indent") !== "2") {
      throw new Error("nested HTML Source indentation is incorrect");
    }

    const sourceRestoreRow = page.locator("[data-document-line]").filter({ hasText: "<h2>Safety checks</h2>" }).first();
    await sourceRestoreRow.evaluate((row) => {
      window.scrollTo({ top: window.scrollY + row.getBoundingClientRect().top - 72, behavior: "auto" });
    });
    await page.waitForTimeout(100);
    const sourceScrollState = await sourceRestoreRow.evaluate((row) => ({
      top: row.getBoundingClientRect().top,
      scrollY: window.scrollY,
      maxScroll: document.documentElement.scrollHeight - window.innerHeight,
      viewport: window.innerHeight,
      articleScrollTop: row.closest("article")?.scrollTop,
    }));
    if (sourceScrollState.scrollY <= 0 || sourceScrollState.top >= sourceScrollState.viewport) {
      throw new Error(`Source test could not establish a scrolled position: ${JSON.stringify(sourceScrollState)}`);
    }
    await page.getByRole("button", { name: "Preview" }).evaluate((button) => button.click());
    const restoredPreviewTarget = preview.getByRole("heading", { name: "Safety checks" });
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
    await previewListItem.evaluate((item) => item.click());
    const elementComposer = page.getByRole("complementary", { name: "HTML preview comment" });
    await elementComposer.waitFor();
    if (
      !listHover.anchor?.startLine ||
      !((await elementComposer.innerText()).includes(`Line ${listHover.anchor.startLine}`))
    ) {
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
    const composerPlacement = await previewComposer.evaluate((composer) => {
      const rect = composer.getBoundingClientRect();
      const shell = composer.closest(".html-preview-frame-shell")?.getBoundingClientRect();
      return { top: rect.top, bottom: rect.bottom, shellTop: shell?.top, shellBottom: shell?.bottom };
    });
    if (
      composerPlacement.shellTop === undefined ||
      composerPlacement.shellBottom === undefined ||
      composerPlacement.top < composerPlacement.shellTop ||
      composerPlacement.bottom > composerPlacement.shellBottom + 1
    ) {
      throw new Error(`HTML draft composer escaped the iframe viewport: ${JSON.stringify(composerPlacement)}`);
    }
    const selectionHoverState = await page.evaluate(() => window.__planmaxxHoverEvents.at(-1));
    if (selectionHoverState?.state !== "none") {
      throw new Error(`text range selection did not suppress the element hover target: ${JSON.stringify(selectionHoverState)}`);
    }
    const previewDraftField = previewComposer.getByPlaceholder("Leave a comment for this selection...");
    await page.waitForFunction(() =>
      document.activeElement?.getAttribute("placeholder") === "Leave a comment for this selection...",
    );
    await previewDraftField.fill("Draft text must survive a new HTML selection");
    await previewListItem.evaluate((item) => {
      const text = [...item.childNodes].find((node) => node.nodeType === Node.TEXT_NODE);
      if (!text) throw new Error("HTML preview list item has no text node");
      const start = text.data.indexOf("iterate");
      if (start < 0) throw new Error("HTML preview list item is missing the retarget text");
      const range = document.createRange();
      range.setStart(text, start);
      range.setEnd(text, start + "iterate".length);
      const selection = document.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    });
    await page.waitForFunction(() => {
      const field = document.querySelector('textarea[placeholder="Leave a comment for this selection..."]');
      return field?.value === "Draft text must survive a new HTML selection" && document.activeElement === field;
    });
    await previewDraftField.fill("Can this preserve rollback?");
    await previewComposer.getByRole("button", { name: "/btw", exact: true }).click();
    await page.getByText("Keep the rollback owner explicit and verify the fallback path before rollout.", { exact: true }).waitFor();
    const includeAnswer = page.getByRole("button", { name: "Include answer", exact: true });
    await includeAnswer.click();
    await page.getByText("/btw answer will be used for iteration or approval", { exact: true }).waitFor();
    await preview.locator("body").evaluate(() => document.getSelection()?.removeAllRanges());

    await preview.getByText("Gate", { exact: true }).click();
    await page.getByRole("complementary", { name: "HTML preview comment" }).waitFor();
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByRole("button", { name: "Cancel" }).click();

    await preview.getByText("Deploy", { exact: true }).click();
    await page.getByRole("complementary", { name: "HTML preview comment" }).waitFor();
    await page.getByRole("complementary", { name: "HTML preview comment" }).getByRole("button", { name: "Cancel" }).click();

    await page.getByRole("button", { name: "Alongside" }).click();
    await previewListItem.evaluate((item) => item.scrollIntoView({ block: "center" }));
    const listRailCard = page.locator(".html-preview-rail-comment").filter({ hasText: "Existing list comment" });
    await listRailCard.waitFor();
    const alignedListComment = await Promise.all([
      previewListItem.evaluate((item) => {
        const rect = item.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom };
      }),
      listRailCard.evaluate((card) => {
        const rect = card.getBoundingClientRect();
        return {
          top: rect.top,
          bottom: rect.bottom,
          anchorY: Number.parseFloat(getComputedStyle(card).getPropertyValue("--html-comment-anchor-y")),
        };
      }),
      page.locator(".html-plan-preview").evaluate((frame) => {
        const rect = frame.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom };
      }),
    ]);
    const renderedTargetY = alignedListComment[0].top;
    const railTargetY = alignedListComment[1].top - alignedListComment[2].top + alignedListComment[1].anchorY;
    if (Math.abs(railTargetY - renderedTargetY) > 3) {
      throw new Error(`HTML alongside comment is not aligned with its rendered target: ${JSON.stringify(alignedListComment)}`);
    }
    const railAnchorBeforeScroll = alignedListComment[1].anchorY;
    await preview.locator("body").evaluate(() => window.scrollBy({ top: 28, behavior: "auto" }));
    await page.waitForFunction((previousAnchor) => {
      const card = [...document.querySelectorAll(".html-preview-rail-comment")].find((element) => element.textContent?.includes("Existing list comment"));
      const anchor = card ? Number.parseFloat(getComputedStyle(card).getPropertyValue("--html-comment-anchor-y")) : NaN;
      return Number.isFinite(anchor) && Math.abs(anchor - previousAnchor) > 8;
    }, railAnchorBeforeScroll);
    await previewListItem.evaluate((item) => item.scrollIntoView({ block: "center" }));
    await page.waitForTimeout(100);

    const listThreadID = await listRailCard.getAttribute("data-html-preview-rail-comment");
    await page.evaluate(() => {
      window.__planmaxxFocusEvents = [];
    });
    const listAnnotationRect = await page.evaluate((threadID) => {
      const layouts = window.__planmaxxPreviewLayouts ?? [];
      for (let index = layouts.length - 1; index >= 0; index--) {
        const target = layouts[index].targets?.find((candidate) => candidate.id === threadID)?.target;
        if (target) return target;
      }
      return null;
    }, listThreadID);
    if (!listAnnotationRect) throw new Error(`HTML range annotation did not publish geometry for ${listThreadID}`);
    await preview.locator("body").evaluate((body, rect) => {
      document.getSelection()?.removeAllRanges();
      const clientX = rect.left + rect.width / 2;
      const clientY = rect.top + rect.height / 2;
      const target = document.elementFromPoint(clientX, clientY) ?? body;
      target.dispatchEvent(new MouseEvent("click", {
        bubbles: true,
        cancelable: true,
        clientX,
        clientY,
      }));
    }, listAnnotationRect);
    await page.waitForTimeout(150);
    const previewToCardState = await page.evaluate(() => {
      const card = [...document.querySelectorAll(".html-preview-rail-comment .thread")].find((element) => element.textContent?.includes("Existing list comment"));
      return {
        ping: card?.classList.contains("is-link-ping"),
        focusEvents: window.__planmaxxFocusEvents,
        composers: document.querySelectorAll('[aria-label="HTML preview comment"]').length,
      };
    });
    if (!previewToCardState.ping) {
      throw new Error(`Clicking an HTML range highlight did not ping its alongside card: ${JSON.stringify(previewToCardState)} rect=${JSON.stringify(listAnnotationRect)}`);
    }
    await page.waitForTimeout(800);
    await listRailCard.locator(".thread-card-heading").click();
    await page.waitForFunction(() => {
      const card = [...document.querySelectorAll(".html-preview-rail-comment .thread")].find((element) => element.textContent?.includes("Existing list comment"));
      return card?.classList.contains("is-link-ping");
    });
    const listAfterCardClick = await previewListItem.evaluate((item) => {
      const rect = item.getBoundingClientRect();
      return { top: rect.top, bottom: rect.bottom, viewport: window.innerHeight };
    });
    if (listAfterCardClick.bottom <= 0 || listAfterCardClick.top >= listAfterCardClick.viewport) {
      throw new Error(`Clicking an alongside comment did not scroll to its Preview target: ${JSON.stringify(listAfterCardClick)}`);
    }
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
    await page.getByRole("button", { name: "Source" }).click();
    const sourceListCard = page.locator(".plan-comment-rail .thread").filter({ hasText: "Existing list comment" });
    await sourceListCard.waitFor();
    await sourceListCard.locator(".thread-card-heading").click();
    await page.waitForFunction(() => document.querySelector(".line-row.is-link-ping") !== null);
    const sourceListTarget = page.locator("[data-document-line]").filter({ hasText: "Ship &amp; iterate carefully." }).first();
    const sourceListPosition = await sourceListTarget.evaluate((row) => {
      const rect = row.getBoundingClientRect();
      return { top: rect.top, bottom: rect.bottom, viewport: window.innerHeight };
    });
    if (sourceListPosition.bottom <= 0 || sourceListPosition.top >= sourceListPosition.viewport) {
      throw new Error(`Clicking an alongside comment did not scroll to its Source target: ${JSON.stringify(sourceListPosition)}`);
    }
    await page.getByRole("button", { name: "Preview" }).click();
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
      /^Launch & rollback plan · line \d+$/,
      /^Rollout controls · line \d+$/,
      /^Safety checks · line \d+$/,
    ]) {
      const outlineItem = outline.getByTitle(section);
      const title = await outlineItem.getAttribute("title");
      await outlineItem.click();
      if (await page.getByRole("button", { name: "Preview" }).getAttribute("aria-pressed") !== "true") {
        throw new Error(`HTML outline selection left preview mode: ${title}`);
      }
      if (await page.locator(".html-plan-preview").count() !== 1 || await page.locator(".line-row").count() !== 0) {
        throw new Error(`HTML outline selection rendered source rows instead of Preview: ${title}`);
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
    await composer.waitFor();
    await page.waitForFunction(
      () => document.activeElement?.getAttribute("placeholder") === "Leave a comment for this selection...",
      undefined,
      { timeout: 5_000 },
    ).catch(async (error) => {
      throw new Error(`Markdown selection did not focus the composer: ${error}\nactive=${await page.evaluate(() => document.activeElement?.outerHTML)}`);
    });
    await composer.fill("What is the safest rollout?");
    await rolloutLine.evaluate((line) => {
      const walker = document.createTreeWalker(line, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const start = node.data.indexOf("carefully");
        if (start < 0) continue;
        const range = document.createRange();
        range.setStart(node, start);
        range.setEnd(node, start + "carefully".length);
        const selection = document.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        line.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
        return;
      }
      throw new Error("could not retarget the Markdown rollout selection");
    });
    await page.waitForFunction(() => {
      const field = document.querySelector('textarea[placeholder="Leave a comment for this selection..."]');
      return field?.value === "What is the safest rollout?" && document.activeElement === field;
    }, undefined, { timeout: 5_000 }).catch(async (error) => {
      throw new Error(`Markdown retargeting lost the draft or focus: ${error}\nstate=${JSON.stringify(await page.evaluate(() => {
        const field = document.querySelector('textarea[placeholder="Leave a comment for this selection..."]');
        return { value: field?.value, active: document.activeElement?.outerHTML };
      }))}`);
    });
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
