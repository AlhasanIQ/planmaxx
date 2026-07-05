import type { DiffLine } from "../types";

export function DiffView({
  lines,
  emptyMessage = "No changes.",
}: {
  lines: DiffLine[];
  emptyMessage?: string;
}) {
  if (lines.length === 0) {
    return (
      <div className="diff-empty">
        {emptyMessage}
      </div>
    );
  }

  return (
    <table className="diff-view" aria-label="Line diff">
      <tbody>
        {lines.map((line, index) => (
          <tr
            key={`${line.kind}-${line.before ?? "x"}-${line.after ?? "x"}-${index}`}
            className={`diff-row is-${line.kind}`}
          >
            <td className="diff-num">{line.before ?? ""}</td>
            <td className="diff-num">{line.after ?? ""}</td>
            <td className="diff-mark">
              {line.kind === "add" ? "+" : line.kind === "remove" ? "-" : ""}
            </td>
            <td className="diff-text">
              <code>{line.text || " "}</code>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
