// Codex forwards named environment variables; this maps per-server names without putting values in argv.
const [command, argsJson, namesJson] = process.argv.slice(2);
const names: Record<string, string> = JSON.parse(namesJson);
const env = { ...process.env };
for (const [name, source] of Object.entries(names)) { env[name] = env[source]; delete env[source]; }
const child = Bun.spawn([command, ...JSON.parse(argsJson)], { env, stdin: 'inherit', stdout: 'inherit', stderr: 'inherit' });
for (const signal of ['SIGTERM', 'SIGINT'] as const) process.on(signal, () => child.kill(signal));
process.exit(await child.exited);
