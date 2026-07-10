import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import typescript from "@shikijs/langs/typescript";
import javascript from "@shikijs/langs/javascript";
import json from "@shikijs/langs/json";
import bash from "@shikijs/langs/bash";
import go from "@shikijs/langs/go";
import python from "@shikijs/langs/python";
import yaml from "@shikijs/langs/yaml";
import sql from "@shikijs/langs/sql";
import html from "@shikijs/langs/html";
import css from "@shikijs/langs/css";
import markdown from "@shikijs/langs/markdown";
import diff from "@shikijs/langs/diff";
import dockerfile from "@shikijs/langs/dockerfile";
import toml from "@shikijs/langs/toml";
import githubLight from "@shikijs/themes/github-light";
import githubDark from "@shikijs/themes/github-dark";

export interface HighlightToken {
  content: string;
  color?: string;
  fontStyle?: number;
}

interface CodeBlock {
  language: string;
  startLine: number;
  lines: string[];
}

const highlighter = createHighlighterCore({
  engine: createJavaScriptRegexEngine(),
  langs: [
    ...typescript,
    ...javascript,
    ...json,
    ...bash,
    ...go,
    ...python,
    ...yaml,
    ...sql,
    ...html,
    ...css,
    ...markdown,
    ...diff,
    ...dockerfile,
    ...toml,
  ],
  themes: [githubLight, githubDark],
});

export function codeBlocks(plan: string): CodeBlock[] {
  const blocks: CodeBlock[] = [];
  const lines = plan.split(/\r?\n/);
  let open: { language: string; startLine: number; lines: string[] } | null = null;

  for (let index = 0; index < lines.length; index += 1) {
    const fence = /^\s*```\s*([^\s`]*)/.exec(lines[index]);
    if (fence) {
      if (open) {
        blocks.push(open);
        open = null;
      } else {
        open = { language: fence[1] || "text", startLine: index + 2, lines: [] };
      }
      continue;
    }
    if (open) open.lines.push(lines[index]);
  }
  return blocks;
}

export async function highlightCodeBlocks(
  plan: string,
  theme: "light" | "dark",
): Promise<Map<number, HighlightToken[]>> {
  const highlighted = new Map<number, HighlightToken[]>();
  const shikiTheme = theme === "dark" ? "github-dark" : "github-light";

  await Promise.all(codeBlocks(plan).map(async (block) => {
    try {
      const result = (await highlighter).codeToTokens(block.lines.join("\n"), {
        lang: block.language,
        theme: shikiTheme,
      });
      result.tokens.forEach((tokens, index) => {
        highlighted.set(block.startLine + index, tokens.map(({ content, color, fontStyle }) => ({ content, color, fontStyle })));
      });
    } catch {
      // An unknown fence language should remain readable as plain code.
    }
  }));

  return highlighted;
}
