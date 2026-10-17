// Installability and explicit connectivity status for the existing Web client.
(() => {
  const banner = document.createElement('div');
  banner.id = 'secretary-connection-error';
  banner.setAttribute('role', 'alert');
  banner.hidden = true;
  banner.style.cssText = 'position:fixed;z-index:100;top:env(safe-area-inset-top,0px);left:50%;transform:translateX(-50%);box-sizing:border-box;width:min(92vw,40rem);padding:0.75rem 1rem;border:1px solid #fda4af66;border-radius:0 0 0.75rem 0.75rem;background:#3b1522;color:#ffe4e6;font:14px/1.45 system-ui,sans-serif;box-shadow:0 6px 24px #0008;';

  let generation = 0;
  const report = (message) => {
    banner.textContent = message;
    banner.hidden = !message;
  };
  async function checkConnection() {
    const current = ++generation;
    if (!navigator.onLine) {
      report('You are offline. Secretary requires a connection; offline mode is unavailable.');
      return;
    }
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch('/v1/health', {
        cache: 'no-store', credentials: 'same-origin', signal: controller.signal,
      });
      if (!response.ok) throw new Error('Server health check failed');
      if (current === generation) {
        report('');
        // The Web app may have loaded while the server was unreachable.
        // Notify it on each successful health probe so missed startup events recover too.
        window.dispatchEvent(new Event('secretary:server-available'));
      }
    } catch (_) {
      if (current === generation) {
        report('Secretary Server is unreachable. Conversation and Worker controls require a connection.');
      }
    } finally {
      clearTimeout(timeout);
    }
  }

  function start() {
    document.body.appendChild(banner);
    window.addEventListener('offline', () => {
      ++generation;
      report('You are offline. Secretary requires a connection; offline mode is unavailable.');
    });
    window.addEventListener('online', () => { void checkConnection(); });
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) void checkConnection();
    });
    void checkConnection();
    setInterval(() => { if (!document.hidden) void checkConnection(); }, 15000);
    if (window.isSecureContext && 'serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js', { scope: '/' })
        .catch((error) => console.warn('Secretary PWA registration failed:', error));
    }
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start, { once: true });
  } else {
    start();
  }
})();
