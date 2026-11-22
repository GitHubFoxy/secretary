const cacheName = 'secretary-shell';

async function updateShell() {
  const response = await fetch('/', {cache:'no-store'});
  const html = await response.clone().text();
  if (!response.ok || !html.includes('id="app"')) throw new Error('Shell unavailable');
  const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map(match => match[1]);
  const files = await Promise.all(assets.map(async url => {
    const file = await fetch(url, {cache:'no-store'});
    if (!file.ok) throw new Error('Asset unavailable');
    return [url, file];
  }));
  const cache = await caches.open(cacheName);
  await Promise.all(files.map(([url, file]) => cache.put(url, file)));
  await cache.put('/', response);
}

self.addEventListener('install', event => event.waitUntil(updateShell().then(() => self.skipWaiting())));
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (event.request.mode === 'navigate' && !url.searchParams.has('login')) {
    event.respondWith(caches.open(cacheName).then(async cache => {
      if (url.searchParams.has('update')) await updateShell().catch(() => {});
      return (await cache.match('/')) || fetch(event.request);
    }));
  } else if (url.pathname.startsWith('/assets/')) {
    event.respondWith(caches.open(cacheName).then(async cache => {
      const cached = await cache.match(event.request);
      if (cached) return cached;
      const response = await fetch(event.request);
      if (response.ok) await cache.put(event.request, response.clone());
      return response;
    }));
  }
});
