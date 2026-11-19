import { homedir } from 'node:os';

export const secretaryFolder = `${homedir()}/.secretary`;
export const mcpPort = 3001;
export const mcpUrl = `http://127.0.0.1:${mcpPort}/mcp`;
