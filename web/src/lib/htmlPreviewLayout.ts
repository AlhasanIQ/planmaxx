export function positionHTMLPreviewComments(
  items: Array<{ id: string; targetTop: number | undefined; height: number }>,
  viewportHeight: number,
): Map<string, number> {
  const gap = 10;
  const inset = 8;
  const visible = items
    .filter((item): item is { id: string; targetTop: number; height: number } => Number.isFinite(item.targetTop))
    .sort((left, right) => left.targetTop - right.targetTop);
  const positions = new Map<string, number>();
  let cursor = inset;
  for (const item of visible) {
    const maxTop = Math.max(inset, viewportHeight - inset - Math.min(item.height, viewportHeight - inset * 2));
    const desired = Math.max(inset, Math.min(maxTop, item.targetTop - 14));
    const top = Math.max(cursor, desired);
    positions.set(item.id, top);
    cursor = top + item.height + gap;
  }
  if (visible.length === 0 || cursor <= viewportHeight - inset) return positions;
  let nextTop = viewportHeight - inset;
  for (let index = visible.length - 1; index >= 0; index--) {
    const item = visible[index];
    const current = positions.get(item.id) ?? inset;
    const top = Math.max(inset, Math.min(current, nextTop - item.height));
    positions.set(item.id, top);
    nextTop = top - gap;
  }
  return positions;
}
