// Retry a failed startup against the existing owner cookie, never by replaying bootstrap credentials.
// A real 401/403 means an expired or absent session; transient transport/server failures can recover.
export function isTransientSessionFailure(error) {
  const status = error?.status;
  return status == null || status === 0 || status === 408 || status === 429 || status >= 500;
}

export function createSessionRecovery({ restore, onFailure }) {
  let pending = null;
  let retryCookieSession = false;
  let disposed = false;

  async function attempt(bootstrapToken = '') {
    if (disposed) return false;
    if (pending) return pending;

    const operation = (async () => {
      try {
        await restore(bootstrapToken);
        retryCookieSession = false;
        return true;
      } catch (error) {
        retryCookieSession = isTransientSessionFailure(error);
        onFailure(error);
        return false;
      }
    })();
    pending = operation;
    try {
      return await operation;
    } finally {
      if (pending === operation) pending = null;
    }
  }

  function onServerAvailable() {
    if (!retryCookieSession || pending || disposed) return Promise.resolve(false);
    // Use GET /v1/web/session through restore(''), not POST /v1/web/session.
    return attempt();
  }

  function cancelRetry() { retryCookieSession = false; }
  function dispose() { disposed = true; retryCookieSession = false; }
  return { attempt, onServerAvailable, cancelRetry, dispose };
}
