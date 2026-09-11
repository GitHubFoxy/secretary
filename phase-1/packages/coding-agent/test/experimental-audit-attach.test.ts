import { copyFile, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";
import { BACKGROUND_CONTEXT } from "@earendil-works/pi-agent-core";
import { expect, test, vi } from "vitest";
import { activateBuiltinClientServices, openClientRuntime } from "../src/experimental/client-runtime.ts";
import { type RunningServer, startServer } from "../src/experimental/server.ts";

// Opt-in local replay. Never attaches to or modifies the original session.
const source = process.env.PI_AUDIT_REPLAY_FILE;
test.skipIf(!source)(
	"attaches two clients to a copied audit transcript and fetches its snapshot",
	async () => {
		const root = await mkdtemp("/tmp/par-");
		const agentDir = join(root, "agent");
		const sessionDir = join(root, "sessions");
		let server: RunningServer | undefined;
		const clients: Awaited<ReturnType<typeof openClientRuntime>>[] = [];
		try {
			await mkdir(agentDir);
			await mkdir(join(sessionDir, "--private-tmp--"), { recursive: true });
			await copyFile(source!, join(sessionDir, "--private-tmp--", basename(source!)));
			await writeFile(
				join(agentDir, "models.json"),
				JSON.stringify({
					providers: {
						"openai-codex": {
							baseUrl: "http://127.0.0.1:1",
							api: "openai-responses",
							apiKey: "fixture-only",
							models: [
								{
									id: "gpt-5.6-luna",
									name: "Replay only",
									reasoning: true,
									input: ["text"],
									contextWindow: 200000,
									maxTokens: 4096,
									cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
								},
							],
						},
					},
				}),
			);
			vi.stubEnv("PI_CODING_AGENT_DIR", agentDir);
			vi.stubEnv("PI_OFFLINE", "1");
			server = await startServer({ directory: join(root, "server"), sessionDir });
			for (let index = 0; index < 2; index++) {
				const client = await openClientRuntime({
					command: "client",
					connect: { transport: "unix", path: server.socketPath },
				});
				clients.push(client);
				const services = await activateBuiltinClientServices(client.servers[0]!);
				const sessionId = services.directory.state.value!.sessions[0]!.sessionId;
				await services.management.attach(sessionId, BACKGROUND_CONTEXT);
				const snapshot = await services.transcript.snapshot(BACKGROUND_CONTEXT);
				expect(snapshot.transcript.length).toBeGreaterThan(0);
			}
			expect(server.workerPids.size).toBe(1);
		} finally {
			await Promise.all(clients.map((client) => client.dispose()));
			await server?.close();
			vi.unstubAllEnvs();
			await rm(root, { recursive: true, force: true });
		}
	},
	20000,
);
