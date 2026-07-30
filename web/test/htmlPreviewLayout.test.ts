import { describe, expect, test } from "bun:test";
import { positionHTMLPreviewComments } from "../src/lib/htmlPreviewLayout";

describe("HTML preview alongside comment layout", () => {
  test("keeps cards near their rendered targets without overlapping", () => {
    const positions = positionHTMLPreviewComments([
      { id: "first", targetTop: 80, height: 90 },
      { id: "second", targetTop: 105, height: 100 },
      { id: "third", targetTop: 390, height: 80 },
    ], 500);

    expect(positions.get("first")).toBe(66);
    expect(positions.get("second")).toBeGreaterThanOrEqual(166);
    expect(positions.get("third")).toBeLessThanOrEqual(412);
    expect((positions.get("second") ?? 0) + 100).toBeLessThanOrEqual(positions.get("third") ?? 0);
  });

  test("backs a crowded group away from the bottom edge", () => {
    const positions = positionHTMLPreviewComments([
      { id: "first", targetTop: 420, height: 110 },
      { id: "second", targetTop: 450, height: 110 },
    ], 500);

    expect(positions.get("first")).toBe(262);
    expect(positions.get("second")).toBe(382);
  });

  test("omits targets that are outside the iframe viewport", () => {
    const positions = positionHTMLPreviewComments([
      { id: "visible", targetTop: 40, height: 80 },
      { id: "offscreen", targetTop: undefined, height: 80 },
    ], 400);

    expect(positions.has("visible")).toBe(true);
    expect(positions.has("offscreen")).toBe(false);
  });
});
