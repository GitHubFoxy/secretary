import { expect, test } from 'bun:test';
import { markdownText, speechText, splitSentences } from './speech-text';

test('cleans the spoken copy and preserves link text and inline code', () => {
  const markdown = '# Заголовок\n\n- **Ссылка**: [документация](https://example.org).\n- Открой https://example.org и выполни `bun test`.\n\n```ts\nconsole.log(1)\n```\n\n| Имя | Значение |\n| --- | --- |\n| A | 1 |';
  expect(speechText(markdown)).toBe('Заголовок Ссылка: документация. Открой ссылка в чате и выполни bun test. код в чате таблица в чате');
});

test('splits a whole message into sentences for ordered synthesis', () => {
  expect(splitSentences('Первая фраза. Вторая фраза! Остаток')).toEqual(['Первая фраза.', 'Вторая фраза!', 'Остаток']);
});

test('preserves punctuation after a bare URL and inline code as spoken text', () => {
  expect(speechText('Открой https://example.org. Выполни `https://host/a_b`.')).toBe('Открой ссылка в чате. Выполни https://host/a_b.');
  expect(speechText('До\n```\n```\nПосле')).toBe('До код в чате После');
});

test('notification text keeps list bullets, numbers and identifiers', () => {
  expect(markdownText('## Итог\n\n- **Готово**: `main`\n- поле harness_session_id и *важно*\n1. первый шаг'))
    .toBe('Итог\n\n• Готово: main\n• поле harness_session_id и важно\n1. первый шаг');
});
