export function createUpdateChecker({currentVersion, fetchVersion, isBusy, reload}) {
  let checking = false, available = false, stopped = false;
  return {
    async check() {
      if (checking || stopped) return;
      checking = true;
      try {
        const version = await fetchVersion();
        if (stopped || typeof version !== 'string' || !version) return;
        available = version !== currentVersion;
        if (available && !isBusy()) { stopped = true; reload(); }
      } catch { /* Keep using the current version while offline. */ }
      finally { checking = false; }
    },
    stop() { stopped = true; },
    get available() { return available; },
  };
}
