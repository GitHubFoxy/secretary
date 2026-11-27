import { expect, test } from 'bun:test';
import { filterCommands } from './slash-commands.js';

test('фильтрует команды только после начального слеша', () => {
  const commands = [{name:'Alpha'}, {name:'beta'}];
  expect(filterCommands(commands, '/LP')).toEqual([commands[0]]);
  expect(filterCommands(commands, '/')).toEqual(commands);
  expect(filterCommands(commands, 'текст /a')).toEqual(commands);
  expect(filterCommands(commands, '/alpha текст')).toEqual([]);
});

test('находит команду после переноса и не открывает её внутри URL', () => {
  const commands = [{name:'review'}];
  expect(filterCommands(commands, 'текст\n/rev')).toEqual(commands);
  expect(filterCommands(commands, 'https://example.com/rev')).toEqual([]);
});
