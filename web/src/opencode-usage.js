// Короткие суммы токенов для карточки расхода OpenCode: 950 → «950», 1500 → «1,5 тыс.», 2 500 000 → «2,5 млн».
export function formatTokens(n) {
  if (!Number.isFinite(n) || n <= 0) return '0';
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) return `${(n / 1000).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} тыс.`;
  return `${(n / 1_000_000).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} млн`;
}
