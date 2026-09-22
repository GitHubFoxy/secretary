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
	onclose: ((event: { readonly code: number; readonly reason?: string }) => void) | null = null;
	close(code = 1000, reason = ""): void {
		this.onclose?.({ code, reason });
	}
	emit(value: unknown): void {
		this.onmessage?.({ data: JSON.stringify(value) });
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
			if (url.includes("/v1/conversation"))
				return jsonResponse({ entries: [{ id: "entry-1", seq: 1, body: "hello" }], next_before_seq: null, next_after_seq: null });
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
			if (url.includes("/v1/conversation"))
				return jsonResponse({
					entries: [{ id: "entry-1", seq: 1, body: "paired" }],
					next_before_seq: 1,
					next_after_seq: null,
				});
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
		}
		const snapshot = JSON.parse(String(output.mock.calls[0]?.[0]));
		output.mockRestore();
		expect(snapshot).toMatchObject({
			selected_worker_ref: "worker-7",
			worker: { worker: { worker_ref: "worker-7" } },
			worker_activity: [{ id: "activity-1", seq: 1 }],
		});
		expect(calls.map(({ url }) => url)).toEqual([
			"http://secretary.test/v1/clients/pair",
			"http://secretary.test/v1/clients/client-1/poll",
			"http://secretary.test/v1/clients/client-1/redeem",
			"http://secretary.test/v1/conversation",
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
			if (url.includes("/v1/conversation"))
				return jsonResponse({ entries: [], next_before_seq: null, next_after_seq: null });
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

	test("main routes needs_input response through the selected Worker endpoint", async () => {
		const calls: Array<{ url: string; init?: RequestInit }> = [];
		const fetch = vi.fn(async (input: string | URL, init?: RequestInit) => {
			const url = input.toString();
			calls.push({ url, init });
			if (url.includes("/v1/conversation"))
				return jsonResponse({ entries: [], next_before_seq: null, next_after_seq: null });
			if (url.endsWith("/v1/workers")) return jsonResponse([{ worker_ref: "worker-7", status: "needs_input" }]);
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			if (url.includes("/v1/workers/worker-7/activity")) return jsonResponse([]);
			if (url.endsWith("/v1/workers/worker-7")) return jsonResponse({ worker: { worker_ref: "worker-7" }, turns: [] });
			if (url.endsWith("/v1/workers/worker-7/message")) return jsonResponse({ worker: { worker_ref: "worker-7" }, turns: [] });
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
				"--credential",
				"client-only",
				"--worker-ref",
				"worker-7",
				"--respond-request",
				"request-1",
				"--response",
				"answer",
				"--once",
			]);
		} finally {
			vi.stubGlobal("fetch", originalFetch);
			if (originalExperimental === undefined) delete process.env.PI_EXPERIMENTAL;
			else process.env.PI_EXPERIMENTAL = originalExperimental;
			output.mockRestore();
		}
		const action = calls.find(({ url }) => url.endsWith("/message"));
		expect(action).toBeDefined();
		expect(JSON.parse(String(action?.init?.body))).toMatchObject({ text: "answer", request_id: "request-1" });
		expect((action?.init?.headers as Headers).get("Idempotency-Key")).toBeTruthy();
	});
});

describe("Secretary presentation connection states", () => {
	function conversationRuntime(
		fetch: typeof globalThis.fetch,
		sockets: FakeSocket[],
		extra: Record<string, unknown> = {},
	): SecretaryClientRuntime {
		return new SecretaryClientRuntime({
			clientOptions: {
				baseUrl: "http://secretary.test",
				credential: "client-only",
				fetch,
				reconnectInitialMs: 10,
				webSocketFactory: () => {
					const socket = new FakeSocket();
					sockets.push(socket);
					return socket;
				},
			},
			...extra,
		});
	}

	function conversationFetch(entries: Array<{ id: string; seq: number }>) {
		return vi.fn(async (input: string | URL) => {
			const url = input.toString();
			if (url.includes("/v1/conversation"))
				return jsonResponse({ entries, next_before_seq: null, next_after_seq: null });
			if (url.endsWith("/v1/workers")) return jsonResponse([]);
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			throw new Error(`unexpected ${url}`);
		}) as unknown as typeof globalThis.fetch;
	}

	test("once snapshot opens no sockets", async () => {
		const sockets: FakeSocket[] = [];
		const runtime = conversationRuntime(conversationFetch([{ id: "entry-1", seq: 1 }]), sockets, { live: false });
		const state = await runtime.start();
		expect(state.connection).toBe("connected");
		expect(state.conversation.map((entry) => entry.id)).toEqual(["entry-1"]);
		expect(sockets).toHaveLength(0);
		await runtime.dispose();
	});

	test("revoked close moves presentation to revoked", async () => {
		const sockets: FakeSocket[] = [];
		const runtime = conversationRuntime(conversationFetch([]), sockets);
		await runtime.start();
		expect(runtime.presentation.state.connection).toBe("connected");
		expect(sockets).toHaveLength(1);
		sockets[0]!.close(1008, "Client revoked");
		await new Promise((resolve) => setTimeout(resolve, 20));
		expect(runtime.presentation.state.connection).toBe("revoked");
		expect(sockets).toHaveLength(1);
		await runtime.dispose();
	});

	test("sequence jump triggers canonical resync", async () => {
		let tails = 0;
		const fetch = vi.fn(async (input: string | URL) => {
			const url = input.toString();
			if (url.includes("/v1/conversation") && !url.includes("after_seq")) {
				tails += 1;
				return jsonResponse({ entries: [{ id: "entry-1", seq: 1 }], next_before_seq: 1, next_after_seq: null });
			}
			if (url.includes("/v1/conversation"))
				return jsonResponse({ entries: [], next_before_seq: null, next_after_seq: null });
			if (url.endsWith("/v1/workers")) return jsonResponse([]);
			if (url.endsWith("/v1/approvals")) return jsonResponse([]);
			throw new Error(`unexpected ${url}`);
		}) as unknown as typeof globalThis.fetch;
		const sockets: FakeSocket[] = [];
		const runtime = conversationRuntime(fetch, sockets);
		await runtime.start();
		expect(tails).toBe(1);
		sockets[0]!.emit({ id: "entry-9", seq: 9 });
		await new Promise((resolve) => setTimeout(resolve, 50));
		expect(tails).toBeGreaterThan(1);
		expect(runtime.presentation.state.connection).toBe("connected");
		await runtime.dispose();
	});
});
