import { homedir } from 'node:os';
import { resolve } from 'node:path';

export const secretaryFolder = `${homedir()}/.secretary`;
export const port = Number(process.env.SECRETARY_PORT ?? 3000);
export const previewPort = Number(process.env.SECRETARY_PREVIEW_PORT ?? 3002);
export const previewUrl = process.env.SECRETARY_PREVIEW_URL;
export const mcpPort = Number(process.env.SECRETARY_MCP_PORT ?? 3001);
export const mcpUrl = `http://127.0.0.1:${mcpPort}/mcp`;

// Paths from the owner or an agent: `~` is the home folder, a relative path starts at `base`.
export function hostPath(path: string, base = homedir()) { return resolve(base, path.replace(/^~(?=\/|$)/, homedir())); }
