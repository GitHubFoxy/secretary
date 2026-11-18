// Use the chat's actual bottom, not innerHeight: WebView may already resize its layout.
export function keyboardInset({viewport, container, activeElement}) {
  if (!viewport || !container || !activeElement?.matches?.('#message')) return 0;
  if (viewport.scale && Math.abs(viewport.scale - 1) > 0.01) return 0;
  const visibleBottom = viewport.offsetTop + viewport.height;
  const overlap = container.getBoundingClientRect().bottom - visibleBottom;
  return Math.max(0, Math.round(overlap));
}
