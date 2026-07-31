import { describe, expect, test } from "bun:test";
import { anchorForCommentSelection, sourceBoundariesForRenderedText } from "../src/lib/commentSelection";

describe("anchorForCommentSelection", () => {
  test("retains exact character coordinates", () => {
    expect(anchorForCommentSelection(11, 6, 11, 9)).toEqual({
      startLine: 11,
      startChar: 6,
      endLine: 11,
      endChar: 9,
    });
  });
});

describe("sourceBoundariesForRenderedText", () => {
  test("skips Markdown formatting around rendered text", () => {
    expect(sourceBoundariesForRenderedText("**Alpha**", "Alpha")).toEqual([2, 3, 4, 5, 6, 7]);
  });

  test("includes escaped punctuation in the selected source range", () => {
    expect(sourceBoundariesForRenderedText("x\\|y", "x|y")).toEqual([0, 1, 3, 4]);
  });

  test("includes a complete HTML entity in the selected source range", () => {
    expect(sourceBoundariesForRenderedText("A &amp; B", "A & B")).toEqual([0, 1, 2, 7, 8, 9]);
  });
});
