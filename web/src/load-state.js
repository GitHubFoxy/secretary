const TRANSITIONS = {
  loading: { resolved: 'ready', rejected: 'error' },
  ready: { start: 'loading' },
  error: { start: 'loading' },
};

export function transitionLoad(current, event) {
  const next = TRANSITIONS[current]?.[event];
  if (!next) throw new Error(`Invalid load transition: ${current} -> ${event}`);
  return next;
}
