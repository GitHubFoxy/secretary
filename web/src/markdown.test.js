import { expect, test } from 'bun:test';
import { JSDOM } from 'jsdom';

// DOMPurify работает только при наличии window, поэтому поднимаем jsdom до загрузки модуля.
const dom = new JSDOM('');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
const { renderMarkdown } = await import('./markdown.js');

// Разбираем результат в DOM, чтобы проверять структуру, а не точную строку разметки.
function parse(text) {
  const root = dom.window.document.createElement('div');
  root.innerHTML = renderMarkdown(text);
  return root;
}

test('рендерит заголовки, списки, цитаты, код и таблицы', () => {
  const root = parse('# Заголовок\n\n- один\n- два\n\n> цитата\n\n`код`\n\n```js\nconst a = 1;\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |');
  expect(root.querySelector('h1').textContent).toBe('Заголовок');
  expect(root.querySelectorAll('ul > li')).toHaveLength(2);
  expect(root.querySelector('blockquote').textContent.trim()).toBe('цитата');
  expect(root.querySelector('p > code').textContent).toBe('код');
  expect(root.querySelector('pre > code.language-js').textContent).toContain('const a = 1;');
  expect(root.querySelectorAll('th')).toHaveLength(2);
  expect(root.querySelector('td').textContent).toBe('1');
});

test('одиночный перенос строки становится переносом, как в чате', () => {
  expect(parse('первая\nвторая').querySelector('br')).not.toBeNull();
});

test('ссылки открываются в новой вкладке без передачи адреса окна', () => {
  const link = parse('[сайт](https://example.com/a?b=1)').querySelector('a');
  expect(link.getAttribute('href')).toBe('https://example.com/a?b=1');
  expect(link.getAttribute('target')).toBe('_blank');
  expect(link.getAttribute('rel')).toBe('noopener noreferrer nofollow');
});

test('убирает опасные ссылки javascript: и data:', () => {
  const root = parse('[клик](javascript:alert(1)) [данные](data:text/html;base64,PHNjcmlwdD4=)');
  for (const link of root.querySelectorAll('a')) {
    expect(link.getAttribute('href') ?? '').not.toMatch(/^(javascript|data):/i);
  }
});

test('удаляет сырой HTML со скриптами и обработчиками событий', () => {
  const root = parse('текст <script>alert(1)</script><img src=x onerror=alert(2)><iframe src="https://evil.example"></iframe><b onclick="alert(3)">жирный</b>');
  expect(root.querySelector('script')).toBeNull();
  expect(root.querySelector('iframe')).toBeNull();
  expect(root.querySelector('img')).toBeNull();
  expect(root.querySelector('b').hasAttribute('onclick')).toBe(false);
  expect(root.textContent).toContain('жирный');
});

test('не загружает картинки из Markdown', () => {
  const root = parse('![пример](https://tracker.example/pixel.png)');
  expect(root.querySelector('img')).toBeNull();
  expect(root.querySelector('a').textContent).toBe('пример');
  expect(root.querySelector('a').getAttribute('href')).toBe('https://tracker.example/pixel.png');
});

test('в блоке кода HTML показывается текстом, а не выполняется', () => {
  const root = parse('```html\n<script>alert(1)</script>\n```');
  expect(root.querySelector('script')).toBeNull();
  expect(root.querySelector('pre').textContent).toContain('<script>alert(1)</script>');
});

test('чекбоксы списка задач остаются неактивными', () => {
  const input = parse('- [x] готово\n- [ ] нет').querySelector('input[type=checkbox]');
  expect(input.hasAttribute('disabled')).toBe(true);
});
