import { expect, test } from 'bun:test';
import { notePreview, noteSessionId, noteTitle, openedNote, shouldReloadNotes } from './notes.js';

test('заголовок: поле title показывается в списке и в окне', () => {
  expect(noteTitle({ title: 'План', text: 'первая строка\nвторая' })).toBe('План');
});

test('заголовок: старые заметки без title показывают начало текста', () => {
  expect(noteTitle({ title: '', text: 'первая строка\nвторая' })).toBe('первая строка');
  expect(noteTitle({ text: 'первая строка\nвторая' })).toBe('первая строка');
  expect(noteTitle({ title: '   ', text: '  первая строка  \nвторая' })).toBe('первая строка');
});

test('заголовок: пустая заметка называется «Новая заметка»', () => {
  expect(noteTitle({ title: '', text: '' })).toBe('Новая заметка');
});

test('подзаголовок: при заголовке видно начало текста, иначе остаток', () => {
  expect(notePreview({ title: 'План', text: 'первая\nвторая' })).toBe('первая вторая');
  expect(notePreview({ title: '', text: 'первая\nвторая' })).toBe('вторая');
  expect(notePreview({ text: 'только одна строка' })).toBe('');
});

test('открытие и закрытие: отдельное окно показывает выбранную заметку', () => {
  const notes = [{ id: 'a', title: 'План', text: 'текст' }, { id: 'b', title: '', text: 'другая' }];
  expect(openedNote(notes, null)).toBeNull();
  expect(openedNote(notes, 'b')).toEqual({ id: 'b', title: '', text: 'другая' });
  // Закрытие окна возвращает к списку, удалённая заметка не открывается.
  expect(openedNote(notes, 'gone')).toBeNull();
});

test('вид «все заметки»: у Secretary Agent список обновляется при изменении в любой Session', () => {
  const changed = sessionId => ({ type: 'notesChanged', sessionId });
  expect(shouldReloadNotes(changed(2), 1, true)).toBe(true);
  expect(shouldReloadNotes(changed(1), 1, true)).toBe(true);
  expect(shouldReloadNotes(changed(2), 1, false)).toBe(false);
  expect(shouldReloadNotes(changed(2), 2, false)).toBe(true);
  expect(shouldReloadNotes({ type: 'message', sessionId: 2 }, 1, true)).toBe(false);
});

test('сохранение идёт в Session-владелицу заметки', () => {
  expect(noteSessionId({ id: 'a', session_id: 3 }, 1)).toBe(3);
  expect(noteSessionId({ id: 'a' }, 1)).toBe(1);
  expect(noteSessionId(null, 1)).toBe(1);
});
