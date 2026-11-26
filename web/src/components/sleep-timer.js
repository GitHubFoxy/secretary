export function sleepState(deadline, now = Date.now()) {
  if (!deadline) return { expired: false, volume: 1 };
  const remaining = deadline - now;
  return { expired: remaining <= 0, volume: Math.max(0, Math.min(1, remaining / 10000)) };
}
