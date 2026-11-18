import { expect, test } from 'bun:test';
import { allowedMcpRequest } from './mcp-access';

function request(headers: Record<string, string>) {
  return new Request('http://127.0.0.1:3001/mcp', {method:'POST', headers});
}

test('MCP принимает локальный JSON без Origin и отклоняет браузерные запросы', () => {
  expect(allowedMcpRequest(request({host:'127.0.0.1:3001', 'content-type':'application/json'}))).toBe(true);
  expect(allowedMcpRequest(request({host:'localhost:3001', 'content-type':'application/json; charset=utf-8'}))).toBe(true);
  expect(allowedMcpRequest(request({host:'127.0.0.1:3001', origin:'https://example.com', 'content-type':'text/plain'}))).toBe(false);
  expect(allowedMcpRequest(request({host:'127.0.0.1:3001', origin:'https://example.com', 'content-type':'application/json'}))).toBe(false);
  expect(allowedMcpRequest(request({host:'127.0.0.1:3001', origin:'null', 'content-type':'application/json'}))).toBe(false);
  expect(allowedMcpRequest(request({host:'127.0.0.1:3001', 'content-type':'text/plain'}))).toBe(false);
  expect(allowedMcpRequest(request({host:'attacker.example:3001', 'content-type':'application/json'}))).toBe(false);
});
