const INLINE_COMMENT_COMPOSER_LINE_OFFSET = 3;

interface InlineCommentComposerPlacement {
  afterLine: number;
  spacerLines: number;
}

export function inlineCommentComposerPlacement(
  selectedEndLine: number,
  totalLines: number,
): InlineCommentComposerPlacement {
  const safeTotalLines = Math.max(1, Math.trunc(totalLines));
  const targetLine = Math.max(1, Math.trunc(selectedEndLine) + INLINE_COMMENT_COMPOSER_LINE_OFFSET);

  return {
    afterLine: Math.min(targetLine, safeTotalLines),
    spacerLines: Math.max(0, targetLine - safeTotalLines),
  };
}
