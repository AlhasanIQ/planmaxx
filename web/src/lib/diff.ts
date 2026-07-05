import { diffArrays } from "diff";
import type { DiffLine } from "../types";

export function lineDiff(before: string, after: string): DiffLine[] {
  const changes = diffArrays(splitLines(before), splitLines(after));
  const lines: DiffLine[] = [];
  let beforeLine = 1;
  let afterLine = 1;

  for (const change of changes) {
    const kind = change.added ? "add" : change.removed ? "remove" : "context";
    for (const text of change.value) {
      if (kind === "context") {
        lines.push({ kind, before: beforeLine, after: afterLine, text });
        beforeLine += 1;
        afterLine += 1;
      } else if (kind === "remove") {
        lines.push({ kind, before: beforeLine, text });
        beforeLine += 1;
      } else {
        lines.push({ kind, after: afterLine, text });
        afterLine += 1;
      }
    }
  }

  return lines;
}

function splitLines(text: string): string[] {
  if (text === "") return [];
  return text.split("\n");
}
