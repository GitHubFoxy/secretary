import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Secrets } from './secrets';

type Role = 'secretary' | 'specialist';
export type McpServer = { name: string; enabled: boolean; roles: Role[] } & (
  | { type: 'stdio'; command: string; args: string[]; env: Record<string, string> }
  | { type: 'http'; url: string; headers: Record<string, string> }
);

export class ExternalMcp {
  private path: string;
  constructor(private root: string) { this.path = join(root, 'mcp.json'); }
  list(): McpServer[] {
    try { const data = JSON.parse(readFileSync(this.path, 'utf8')); if (!Array.isArray(data)) throw new Error(); return data.map(validate); }
    catch (e: any) { if (e.code === 'ENOENT') return []; throw new Error('Не удалось прочитать реестр MCP'); }
  }
  save(servers: unknown) {
    if (!Array.isArray(servers)) throw new Error('Некорректный список MCP');
    const entries = servers.map(validate);
    if (new Set(entries.map(s => s.name)).size !== entries.length) throw new Error('Имена MCP должны быть уникальны');
    mkdirSync(this.root, { recursive: true, mode: 0o700 });
    const tmp = `${this.path}.${crypto.randomUUID()}`;
    try { writeFileSync(tmp, JSON.stringify(entries, null, 2), { mode: 0o600, flag: 'wx' }); renameSync(tmp, this.path); }
    catch { try { require('node:fs').unlinkSync(tmp); } catch {} throw new Error('Не удалось сохранить реестр MCP'); }
  }
  async resolve(role: Role) {
    const secrets = new Secrets(this.root);
    const substitute = async (values: Record<string, string>) => Object.fromEntries(await Promise.all(Object.entries(values).map(async ([key, value]) => {
      const matches = [...value.matchAll(/\$\{secret:([A-Za-z_][A-Za-z0-9_]*)\}/g)];
      for (const match of matches) {
        const secret = await secrets.get(match[1]);
        if (secret === undefined) throw new Error(`Секрет ${match[1]} не найден`);
        value = value.replaceAll(match[0], secret);
      }
      return [key, value];
    })));
    return Promise.all(this.list().filter(s => s.enabled && s.roles.includes(role)).map(async s => s.type === 'stdio'
      ? { ...s, env: await substitute(s.env) } : { ...s, headers: await substitute(s.headers) }));
  }
}

export async function prepareMcp(root: string | undefined, role: Role | undefined) {
  const env = { ...process.env };
  const servers = root && role ? await new ExternalMcp(root).resolve(role) : [];
  return { env, servers: servers.map(server => {
    const values = server.type === 'stdio' ? server.env : server.headers;
    const mapped = Object.fromEntries(Object.entries(values).map(([name, value]) => {
      const key = `SECRETARY_MCP_${crypto.randomUUID().replaceAll('-', '')}`;
      env[key] = value;
      return [name, key];
    }));
    return {server, mapped};
  }) };
}

function validate(input: any): McpServer {
  if (!input || typeof input !== 'object' || typeof input.name !== 'string' || !/^[A-Za-z0-9_-]+$/.test(input.name) || input.name === 'secretary' || typeof input.enabled !== 'boolean' || !Array.isArray(input.roles) || input.roles.some((r: unknown) => r !== 'secretary' && r !== 'specialist') || new Set(input.roles).size !== input.roles.length) throw new Error('Некорректная запись MCP');
  const values = (v: any, env: boolean) => {
    if (!v || typeof v !== 'object' || Array.isArray(v) || Object.entries(v).some(([k, value]) => !(env ? /^[A-Za-z_][A-Za-z0-9_]*$/.test(k) : /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(k)) || typeof value !== 'string' || /[\r\n]/.test(value))) throw new Error('Некорректные env или headers');
    return Object.fromEntries(Object.entries(v));
  };
  const base = { name: input.name, enabled: input.enabled, roles: input.roles };
  if (input.type === 'stdio' && typeof input.command === 'string' && input.command.trim() && Array.isArray(input.args) && input.args.every((v: unknown) => typeof v === 'string')) return { ...base, type: 'stdio', command: input.command, args: input.args, env: values(input.env, true) as Record<string, string> };
  if (input.type === 'http' && typeof input.url === 'string') {
    let url: URL; try { url = new URL(input.url); } catch { throw new Error('Некорректный URL MCP'); }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Некорректный URL MCP');
    return { ...base, type: 'http', url: input.url, headers: values(input.headers, false) as Record<string, string> };
  }
  throw new Error('Некорректный транспорт MCP');
}
