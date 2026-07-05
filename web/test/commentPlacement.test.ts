import { describe, expect, test } from "bun:test";
import { inlineCommentComposerPlacement } from "../src/lib/commentPlacement";

describe("inlineCommentComposerPlacement", () => {
  test("places the composer three rendered lines after the selected end line", () => {
    expect(inlineCommentComposerPlacement(4, 12)).toEqual({
      afterLine: 7,
      spacerLines: 0,
    });
  });

  test("adds bottom spacer lines when the document ends before the placement target", () => {
    expect(inlineCommentComposerPlacement(9, 10)).toEqual({
      afterLine: 10,
      spacerLines: 2,
    });
  });
});
