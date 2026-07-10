import { Marked, type Tokens } from "marked";

const escapeHTML = (raw: string) =>
  raw
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");

const escapeRawHTMLText = (raw: string) => escapeHTML(raw).replace(/=/g, "&#61;");

const allowedHrefProtocols = new Set(["http:", "https:", "mailto:"]);

const inlineMarkdown = new Marked({
  gfm: true,
  breaks: false,
  renderer: {
    html({ text }: Tokens.HTML | Tokens.Tag) {
      return escapeRawHTMLText(text);
    },
    link({ href, title, tokens }: Tokens.Link) {
      const label = this.parser.parseInline(tokens);
      const normalizedHref = decodeHTMLEntities(href).trim();
      if (!safeHref(normalizedHref)) {
        return label;
      }
      const titleAttribute = title ? ` title="${escapeHTML(title)}"` : "";
      return `<a href="${escapeHTML(normalizedHref)}"${titleAttribute}>${label}</a>`;
    },
    image({ text }: Tokens.Image) {
      return escapeHTML(text);
    },
  },
});

function renderInlineMarkdown(raw: string): string {
  return inlineMarkdown.parseInline(raw) as string;
}

function decodeHTMLEntities(raw: string): string {
  return raw.replace(/&(#x[0-9a-f]+|#\d+|amp|quot|apos|#39|lt|gt);/gi, (_, entity: string) => {
    const normalized = entity.toLowerCase();
    if (normalized === "amp") return "&";
    if (normalized === "quot") return "\"";
    if (normalized === "apos" || normalized === "#39") return "'";
    if (normalized === "lt") return "<";
    if (normalized === "gt") return ">";
    if (normalized.startsWith("#x")) {
      return codePointToString(Number.parseInt(normalized.slice(2), 16), `&${entity};`);
    }
    if (normalized.startsWith("#")) {
      return codePointToString(Number.parseInt(normalized.slice(1), 10), `&${entity};`);
    }
    return `&${entity};`;
  });
}

function codePointToString(value: number, fallback: string): string {
  if (!Number.isFinite(value)) {
    return fallback;
  }
  try {
    return String.fromCodePoint(value);
  } catch {
    return fallback;
  }
}

function safeHref(href: string): boolean {
  const trimmed = href.trim();
  if (trimmed === "") {
    return false;
  }
  if (/[\u0000-\u001f\u007f\\]/.test(trimmed)) {
    return false;
  }
  if (trimmed.startsWith("//")) {
    return false;
  }
  try {
    const url = new URL(trimmed, "http://planmaxx.local");
    if (url.origin === "http://planmaxx.local" && !/^[a-z][a-z0-9+.-]*:/i.test(trimmed)) {
      return true;
    }
    return allowedHrefProtocols.has(url.protocol);
  } catch {
    return false;
  }
}

// Plan is rendered line-by-line so the gutter stays aligned with source. We
// keep this rendering "inline-ish": headings, list markers, code-fence rows
// all get visual treatment but each source line remains its own row, which
// keeps the comment anchor mapping correct.
export interface LineRender {
  kind: "blank" | "heading" | "list" | "code" | "hr" | "quote" | "table-header" | "table-divider" | "table-row" | "text";
  level?: number; // for heading
  indent?: number; // for list
  marker?: string; // for list (e.g., "-", "1.")
  html: string; // inner HTML for content area
}

export function renderPlanLines(plan: string): LineRender[] {
  const lines = plan.split(/\r?\n/);
  if (lines.length > 1 && lines[lines.length - 1] === "") {
    lines.pop();
  }
  let inFence = false;
  const rendered: LineRender[] = [];
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (line.trim().startsWith("```")) {
      inFence = !inFence;
      rendered.push({
        kind: "code",
        html: `<span class="text-foreground-muted">${escapeHTML(line)}</span>`,
      } satisfies LineRender);
      continue;
    }
    if (inFence) {
      rendered.push({
        kind: "code",
        html: `<span class="font-mono text-[12.5px]">${escapeHTML(line) || "&nbsp;"}</span>`,
      } satisfies LineRender);
      continue;
    }
    if (line.trim() === "") {
      rendered.push({ kind: "blank", html: "" } satisfies LineRender);
      continue;
    }
    const table = tableAt(lines, index);
    if (table) {
      rendered.push(...table.lines);
      index += table.consumed - 1;
      continue;
    }
    if (/^\s*---+\s*$/.test(line)) {
      rendered.push({
        kind: "hr",
        html: `<span class="block w-full border-t border-border my-1"></span>`,
      } satisfies LineRender);
      continue;
    }
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      const inner = renderInlineMarkdown(heading[2]);
      const sizes: Record<number, string> = {
        1: "text-2xl font-semibold tracking-tight",
        2: "text-xl font-semibold tracking-tight",
        3: "text-base font-semibold",
        4: "text-sm font-semibold",
        5: "text-sm font-semibold text-foreground-muted",
        6: "text-xs font-semibold uppercase tracking-wider text-foreground-muted",
      };
      rendered.push({
        kind: "heading",
        level,
        html: `<span class="${sizes[level] ?? sizes[3]}">${inner}</span>`,
      } satisfies LineRender);
      continue;
    }
    const quote = /^>\s?(.*)$/.exec(line);
    if (quote) {
      const inner = renderInlineMarkdown(quote[1] || "");
      rendered.push({
        kind: "quote",
        html: `<span class="border-l-2 border-border-strong pl-3 text-foreground-muted block">${inner}</span>`,
      } satisfies LineRender);
      continue;
    }
    const listMatch = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(line);
    if (listMatch) {
      const indent = listMatch[1].length;
      const marker = listMatch[2];
      const rest = renderInlineMarkdown(listMatch[3]);
      const isOrdered = /\d/.test(marker);
      const bullet = isOrdered ? marker : "•";
      const pad = Math.max(0, Math.floor(indent / 2));
      rendered.push({
        kind: "list",
        indent,
        marker,
        html: `<span style="padding-left:${pad * 16}px" class="block"><span class="inline-block w-5 text-foreground-muted">${escapeHTML(bullet)}</span>${rest}</span>`,
      } satisfies LineRender);
      continue;
    }
    rendered.push({
      kind: "text",
      html: renderInlineMarkdown(line),
    } satisfies LineRender);
  }
  return rendered;
}

function tableAt(lines: string[], start: number): { consumed: number; lines: LineRender[] } | null {
  const source = lines.slice(start).join("\n");
  const first = inlineMarkdown.lexer(source)[0];
  if (!first || first.type !== "table") return null;

  const columns = first.header.length;
  if (columns === 0) return null;
  // Marked includes the table's terminating newline in token.raw when another
  // block follows. Counting raw.split("\n") then mistakes that separator for
  // a table row and drops otherwise-valid tables. GFM tables are one header,
  // one delimiter, and one source line per parsed row.
  const consumed = first.rows.length + 2;
  if (consumed > lines.length-start) return null;

  return {
    consumed,
    lines: [
      { kind: "table-header", html: tableMarkup(first.header, columns, "header") },
      { kind: "table-divider", html: tableDividerMarkup(columns) },
      ...first.rows.map((cells) => ({
        kind: "table-row" as const,
        html: tableMarkup(cells, columns, "body"),
      })),
    ],
  };
}

function tableMarkup(cells: Tokens.TableCell[], columns: number, rowKind: "header" | "body"): string {
  const grid = `repeat(${columns}, minmax(9rem, 1fr))`;
  const cellsHTML = cells.map((cell) => {
    const alignment = cell.align ? ` is-${cell.align}` : "";
    return `<span class="plan-table-cell${alignment}">${renderInlineMarkdown(cell.text)}</span>`;
  }).join("");
  return `<span class="plan-table-row is-${rowKind}" style="grid-template-columns:${grid};min-width:${columns * 144}px">${cellsHTML}</span>`;
}

function tableDividerMarkup(columns: number): string {
  const grid = `repeat(${columns}, minmax(9rem, 1fr))`;
  const cells = Array.from({ length: columns }, () => "<span class=\"plan-table-divider-cell\" aria-hidden=\"true\">&nbsp;</span>").join("");
  return `<span class="plan-table-row is-divider" style="grid-template-columns:${grid};min-width:${columns * 144}px">${cells}</span>`;
}

export function planExcerpt(plan: string, startLine: number, endLine: number): string {
  const start = Math.max(1, startLine);
  const end = Math.max(start, endLine);
  return plan.split(/\r?\n/).slice(start - 1, end).join("\n");
}
