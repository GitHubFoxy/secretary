import { expect, test } from 'bun:test';
import { speechText, splitSentences } from './speech-text';

test('cleans the spoken copy and preserves link text and inline code', () => {
  const markdown = '# Заголовок\n\n- **Ссылка**: [документация](https://example.org).\n- Открой https://example.org и выполни `bun test`.\n\n```ts\nconsole.log(1)\n```\n\n| Имя | Значение |\n| --- | --- |\n| A | 1 |';
  expect(speechText(markdown)).toBe('Заголовок Ссылка: документация. Открой ссылка в чате и выполни bun test. код в чате таблица в чате');
});

test('sends complete sentences as text arrives and flushes the remainder', () => {
  let buffer = '', spoken: string[] = [];
  for (const chunk of ['Первая фраза.', ' Вторая ', 'фраза! Остаток']) {
    buffer += chunk;
    const result = splitSentences(buffer);
    spoken.push(...result.sentences);
    buffer = result.rest;
  }
  expect(spoken).toEqual(['Первая фраза.', 'Вторая фраза!']);
  expect(splitSentences(buffer, true)).toEqual({sentences:['Остаток'], rest:''});
});
