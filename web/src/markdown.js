import { marked } from 'marked';
import DOMPurify from 'dompurify';

// Разбираем Markdown с переносами строк как в чате: одиночный перенос остаётся переносом.
const options = { gfm: true, breaks: true, async: false, renderer: new marked.Renderer() };
options.renderer.image = ({ href, text }) => `<a href="${escapeHtml(href)}">${escapeHtml(text || 'Изображение')}</a>`;

// Разрешаем только безопасный набор тегов. Сырой HTML из ответа агента очищается по этому списку.
// Картинки показываем ссылками, чтобы не загружать их с чужих серверов.
const ALLOWED_TAGS = [
  'p', 'br', 'hr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'strong', 'b', 'em', 'i', 'del', 's', 'code', 'pre',
  'blockquote', 'ul', 'ol', 'li', 'a', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'input',
];
const ALLOWED_ATTR = ['href', 'title', 'class', 'start', 'colspan', 'rowspan', 'type', 'checked', 'disabled'];

// Ссылки открываются в новой вкладке и не передают адрес чата и не получают доступ к окну чата.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer nofollow');
  }
});

function escapeHtml(text) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// Возвращает безопасный HTML для текста сообщения. Если очистка недоступна, показываем текст без разметки.
export function renderMarkdown(text) {
  const source = String(text ?? '');
  if (!DOMPurify.isSupported) return escapeHtml(source).replace(/\n/g, '<br>');
  const html = marked.parse(source, options);
  return DOMPurify.sanitize(html, { ALLOWED_TAGS, ALLOWED_ATTR });
}
