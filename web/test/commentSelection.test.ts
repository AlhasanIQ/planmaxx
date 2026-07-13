import { describe, expect, test } from "bun:test";
import { anchorForCommentSelection } from "../src/lib/commentSelection";

describe("anchorForCommentSelection", () => {
  test("uses complete rows for a table selection so source Markdown offsets cannot drift", () => {
    expect(anchorForCommentSelection(6, 4, 6, 10, true)).toEqual({ startLine: 6, endLine: 6 });
  });

  test("uses complete rows when a selection crosses a table boundary", () => {
    expect(anchorForCommentSelection(5, 2, 7, 8, true)).toEqual({ startLine: 5, endLine: 7 });
  });

  test("retains exact character coordinates for a code-block selection", () => {
    expect(anchorForCommentSelection(11, 6, 11, 9, false)).toEqual({
      startLine: 11,
      startChar: 6,
      endLine: 11,
      endChar: 9,
    });
  });
});
