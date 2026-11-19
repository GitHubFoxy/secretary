import { expect, test } from 'bun:test';
import { showInSidebar } from './sessions.js';

test('hides the main Secretary chat and every session inside a .secretary folder', () => {
  expect(showInSidebar({ id: 1, folder: '/home/coder/.secretary' })).toBe(false);
  expect(showInSidebar({ id: 8, folder: '/home/coder/.secretary' })).toBe(false);
  expect(showInSidebar({ id: 9, folder: '/home/coder/.secretary/agents/foo/' })).toBe(false);
  expect(showInSidebar({ id: 13, folder: '/home/coder/.secretary/agents/foo/bar' })).toBe(false);
  expect(showInSidebar({ id: 14, folder: '.secretary' })).toBe(false);
  expect(showInSidebar({ id: 10, folder: '/home/coder/projects/secretary' })).toBe(true);
  expect(showInSidebar({ id: 11, folder: '/home/coder/.secretary-backup' })).toBe(true);
  expect(showInSidebar({ id: 12, folder: '/home/coder/personal' })).toBe(true);
});
