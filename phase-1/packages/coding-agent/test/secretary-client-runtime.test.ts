import { describe, expect, test, vi } from "vitest";
import { main } from "../src/main.ts";
import type { SecretaryWebSocket } from "../src/secretary/client.ts";
import { SecretaryClientRuntime } from "../src/secretary/runtime.ts";

class FakeSocket implements SecretaryWebSocket {
	readonly OPEN = 1;
	readonly readyState = 1;
	onopen: (() => void) | null = null;
	onmessage: ((event: { readonly data: unknown }) => void) | null = null;
	onerror: ((event: unknown) => void) | null = null;
	onclose: (() => void) | null = null;
	close(): void {
		this.onclose?.();
	}
}

function jsonResponse(value: unknown): Response {
	return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

describe("production Secretary client entrypoint", () => {
	test("loads conversation replay and Worker observer without Node protocol or spawn", async () => {
		const calls: string[] = [];
		const fetch = vi.fn(async (input: string | URL) => {
			const url = input.toString();
			calls.push(url);
			if (url.includes("/v1/conversation")) return jsonResponse([{ id: "entry-1", seq: 1, body: "hello" }]);
			if (url.endsWith("/v1/workers")) return jsonResponse([{ worker_ref: "worker-7", status: "needs_input" }]);
			if (url.includes("/v1/workers/worker-7/activity"))
				return jsonResponse([{ id: "activity-1", seq: 1, kind: "needs_input", request_id: "request-1" }]);
			if (url.endsWith("/v1/workers/worker-7"))
				return jsonResponse({ worker: { worker_ref: "worker-7", status: "needs_input" }, turns: [] });
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			throw new Error(`unexpected ${url}`);
		}) as unknown as typeof globalThis.fetch;
		const sockets: FakeSocket[] = [];
		const runtime = new SecretaryClientRuntime({
			clientOptions: {
				baseUrl: "http://secretary.test",
				credential: "client-only",
				fetch,
				reconnectInitialMs: 10,
				webSocketFactory: (url, credential) => {
					expect(credential).toBe("client-only");
					expect(url).toContain("/v1/workers/worker-7/activity/ws");
					const socket = new FakeSocket();
					sockets.push(socket);
					return socket;
				},
			},
			workerRef: "worker-7",
		});
		await runtime.start();
		expect(runtime.presentation.state.conversation.map((entry) => entry.id)).toEqual(["entry-1"]);
		expect(runtime.presentation.state.selectedWorkerRef).toBe("worker-7");
		expect(runtime.presentation.state.workerActivity[0]).toMatchObject({
			kind: "needs_input",
			request_id: "request-1",
		});
		await runtime.reconnect();
		expect(
			calls.some((url) => url.includes("/spawn") || url.includes("/nodes/connect") || url.includes("node/ws")),
		).toBe(false);
		expect(sockets).toHaveLength(2);
		await runtime.dispose();
	});

	test("main dispatches pairing, replay, and observation through the production adapter", async () => {
		const calls: Array<{ url: string; init?: RequestInit }> = [];
		const fetch = vi.fn(async (input: string | URL, init?: RequestInit) => {
			const url = input.toString();
			calls.push({ url, init });
			if (url.endsWith("/v1/clients/pair"))
				return new Response(JSON.stringify({ client_id: "client-1", pending_token: "pending" }), { status: 201 });
			if (url.endsWith("/v1/clients/client-1/poll"))
				return jsonResponse({ client_id: "client-1", status: "active", credential_ready: true, redeemed: false });
			if (url.endsWith("/v1/clients/client-1/redeem"))
				return jsonResponse({ client_id: "client-1", credential: "client-only", status: "active" });
			if (url.includes("/v1/conversation")) return jsonResponse([{ id: "entry-1", seq: 1, body: "paired" }]);
			if (url.endsWith("/v1/workers")) return jsonResponse([{ worker_ref: "worker-7", status: "needs_input" }]);
			if (url.includes("/v1/workers/worker-7/activity"))
				return jsonResponse([{ id: "activity-1", seq: 1, kind: "needs_input" }]);
			if (url.endsWith("/v1/workers/worker-7"))
				return jsonResponse({ worker: { worker_ref: "worker-7" }, turns: [] });
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			throw new Error(`unexpected ${url}`);
		}) as unknown as typeof globalThis.fetch;
		const originalFetch = globalThis.fetch;
		const originalExperimental = process.env.PI_EXPERIMENTAL;
		const output = vi.spyOn(console, "log").mockImplementation(() => {});
		vi.stubGlobal("fetch", fetch);
		process.env.PI_EXPERIMENTAL = "1";
		try {
			await main([
				"secretary",
				"--base-url",
				"http://secretary.test",
				"--bootstrap-token",
				"bootstrap-only",
				"--device-id",
				"pi-1",
				"--display-name",
				"Pi",
				"--worker-ref",
				"worker-7",
				"--once",
			]);
		} finally {
			vi.stubGlobal("fetch", originalFetch);
			if (originalExperimental === undefined) delete process.env.PI_EXPERIMENTAL;
			else process.env.PI_EXPERIMENTAL = originalExperimental;
			output.mockRestore();
		}
		expect(calls.map(({ url }) => url)).toEqual([
			"http://secretary.test/v1/clients/pair",
			"http://secretary.test/v1/clients/client-1/poll",
			"http://secretary.test/v1/clients/client-1/redeem",
			"http://secretary.test/v1/conversation?after_seq=0",
			"http://secretary.test/v1/workers",
			"http://secretary.test/v1/approvals",
			"http://secretary.test/v1/workers/worker-7",
			"http://secretary.test/v1/workers/worker-7/activity?after_seq=0",
			"http://secretary.test/v1/workers/worker-7/activity?after_seq=1",
		]);
		expect((calls[3]!.init?.headers as Headers).get("Authorization")).toBe("Bearer client-only");
	});

	test("main dispatches the explicit Secretary command to the production adapter", async () => {
		const fetch = vi.fn(async (input: string | URL) => {
			const url = input.toString();
			if (url.includes("/v1/conversation")) return jsonResponse([]);
			if (url.endsWith("/v1/workers")) return jsonResponse([]);
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			throw new Error(`unexpected ${url}`);
		}) as unknown as typeof globalThis.fetch;
		const originalFetch = globalThis.fetch;
		const originalExperimental = process.env.PI_EXPERIMENTAL;
		const output = vi.spyOn(console, "log").mockImplementation(() => {});
		vi.stubGlobal("fetch", fetch);
		process.env.PI_EXPERIMENTAL = "1";
		try {
			await main(["secretary", "--base-url", "http://secretary.test", "--credential", "client-only", "--once"]);
		} finally {
			vi.stubGlobal("fetch", originalFetch);
			if (originalExperimental === undefined) delete process.env.PI_EXPERIMENTAL;
			else process.env.PI_EXPERIMENTAL = originalExperimental;
			output.mockRestore();
		}
		expect(fetch).toHaveBeenCalled();
	});
});
