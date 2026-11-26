const key = 'browser-notifications';
export function notificationsEnabled() {
  try { return localStorage.getItem(key) === 'true' && globalThis.Notification?.permission === 'granted'; }
  catch { return false; }
}
export async function setNotificationsEnabled(enabled) {
  try {
    if (!enabled) { localStorage.removeItem(key); return ''; }
    if (!globalThis.Notification) return 'Уведомления недоступны в этом браузере';
    const permission = await Notification.requestPermission();
    if (permission !== 'granted') return permission === 'denied' ? 'Разрешите уведомления в настройках браузера' : '';
    localStorage.setItem(key, 'true');
    return '';
  } catch { return 'Уведомления недоступны в этом браузере'; }
}
export function shouldNotify({enabled, permission, text, visibility, native}) {
  return enabled && permission === 'granted' && !!text?.trim() && visibility !== 'visible' && !native;
}
