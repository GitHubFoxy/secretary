import { expect, test } from 'bun:test';
import { keyboardInset } from './viewport.js';
const input = {matches: selector => selector === '#message'};
const box = bottom => ({getBoundingClientRect:() => ({bottom})});

test('ignores visual viewport offsets when no composer is focused', () => {
  expect(keyboardInset({viewport:{height:500,offsetTop:0,scale:1},container:box(800),activeElement:null})).toBe(0);
});
test('does not count keyboard twice when layout already resized', () => {
  expect(keyboardInset({viewport:{height:500,offsetTop:0,scale:1},container:box(500),activeElement:input})).toBe(0);
});
test('moves composer only by real visual overlap on iOS-like viewport', () => {
  expect(keyboardInset({viewport:{height:500,offsetTop:10,scale:1},container:box(800),activeElement:input})).toBe(290);
});
test('ignores zoom and other input fields', () => {
  expect(keyboardInset({viewport:{height:400,offsetTop:0,scale:2},container:box(800),activeElement:input})).toBe(0);
  expect(keyboardInset({viewport:{height:400,offsetTop:0,scale:1},container:box(800),activeElement:{matches:()=>false}})).toBe(0);
});
