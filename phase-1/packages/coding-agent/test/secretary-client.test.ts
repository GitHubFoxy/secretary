import { describe, expect, test, vi } from "vitest";
import {
	SecretaryApiError,
	SecretaryClient,
	type SecretaryWebSocket,
	type SecretaryWebSocketFactory,
} from "../src/secretary/client.ts";

const baseUrl = "http://secretary.test";

function response(value: unknown, status = 200): Response {
	return new Response(value === undefined ? null : JSON.stringify(value), {
		status,
		headers: { "content-type": "application/json" },
	});
}

function fetchSequence(responses: readonly Response[]) {
	const calls: Array<{ input: string | URL; init?: RequestInit }> = [];
	let index = 0;
	const fetch = vi.fn(async (input: string | URL, init?: RequestInit) => {
		calls.push({ input, init });
		const result = responses[index++];
		if (result === undefined) throw new Error("unexpected fetch call");
		return result;
	}) as unknown as typeof globalThis.fetch;
	return { fetch, calls };
}

class FakeSocket implements SecretaryWebSocket {
	readonly OPEN = 1;
	readyState = 1;
	onopen: (() => void) | null = null;
	onmessage: ((event: { readonly data: unknown }) => void) | null = null;
	onerror: ((event: unknown) => void) | null = null;
	onclose: ((event: { readonly code: number; readonly reason?: string }) => void) | null = null;
	close(code = 1000, reason = ""): void {
		this.readyState = 3;
		this.onclose?.({ code, reason });
	}
	emit(value: unknown): void {
		this.onmessage?.({ data: JSON.stringify(value) });
	}
	fail(error: unknown): void {
		this.onerror?.(error);
	}
}

function client(fetch: typeof globalThis.fetch, factory?: SecretaryWebSocketFactory): SecretaryClient {
	return new SecretaryClient({
		baseUrl,
		credential: "cli_pi_only",
		fetch,
		webSocketFactory: factory,
		reconnectInitialMs: 10,
		reconnectMaxMs: 10,
	});
}

describe("SecretaryClient", () => {
	test("pairs through the owner-approved handoff and retains only the Client credential", async () => {
		const { fetch, calls } = fetchSequence([
			response({ client_id: "cli-1", pending_token: "pending-only", status: "pending" }, 201),
			response({ client_id: "cli-1", status: "active", credential_ready: true, redeemed: false }),
			response({ client_id: "cli-1", credential: "cli_active", status: "active" }),
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
		]);
		const result = await SecretaryClient.pair({
			baseUrl,
			bootstrapToken: "bootstrap-only",
			deviceId: "pi-1",
			displayName: "Pi",
			fetch,
			pollIntervalMs: 10,
		});
		expect(result.identity).toMatchObject({ id: "cli-1", device_id: "pi-1", status: "active" });
		expect(result.client.clientId).toBe("cli-1");
		expect(result.credential).toBe("cli_active");
		expect(calls[0]!.init?.headers).toBeInstanceOf(Headers);
		expect((calls[1]!.init?.headers as Headers).get("Authorization")).toBe("Bearer pending-only");
		expect((calls[2]!.init?.headers as Headers).get("Authorization")).toBe("Bearer pending-only");
		await result.client.listConversation();
		expect((calls[3]!.init?.headers as Headers).get("Authorization")).toBe("Bearer cli_active");
		expect(JSON.stringify(calls[0]!.init?.body)).toContain("bootstrap-only");
	});

	test("pairs without explicit scopes using the read-only viewer grant", async () => {
		const { fetch, calls } = fetchSequence([
			response({ client_id: "cli-1", pending_token: "pending-only", status: "pending" }, 201),
			response({ client_id: "cli-1", status: "active", credential_ready: true, redeemed: false }),
			response({ client_id: "cli-1", credential: "cli_active", status: "active" }),
		]);
		const result = await SecretaryClient.pair({
			baseUrl,
			bootstrapToken: "bootstrap-only",
			deviceId: "pi-1",
			displayName: "Pi",
			fetch,
			pollIntervalMs: 10,
		});
		const body = JSON.parse(calls[0]!.init?.body as string) as { scopes?: readonly string[] };
		expect(body.scopes).toEqual(["conversation:read", "conversation:write", "worker:read", "worker:message", "approval:read"]);
		expect(result.identity.scopes).toEqual(["conversation:read", "conversation:write", "worker:read", "worker:message", "approval:read"]);
	});

	test("pairs with explicit scopes untouched", async () => {
		const { fetch, calls } = fetchSequence([
			response({ client_id: "cli-1", pending_token: "pending-only", status: "pending" }, 201),
			response({ client_id: "cli-1", status: "active", credential_ready: true, redeemed: false }),
			response({ client_id: "cli-1", credential: "cli_active", status: "active" }),
		]);
		await SecretaryClient.pair({
			baseUrl,
			bootstrapToken: "bootstrap-only",
			deviceId: "pi-1",
			displayName: "Pi",
			scopes: ["worker:read"],
			fetch,
			pollIntervalMs: 10,
		});
		const body = JSON.parse(calls[0]!.init?.body as string) as { scopes?: readonly string[] };
		expect(body.scopes).toEqual(["worker:read"]);
	});

	test("replays ordered conversation entries and ignores the replay/live duplicate", async () => {
		const { fetch, calls } = fetchSequence([
			response({
				entries: [
					{ id: "e2", seq: 2 },
					{ id: "e1", seq: 1 },
				],
				next_before_seq: null,
				next_after_seq: 2,
			}),
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
		]);
		const sockets: FakeSocket[] = [];
		const factory: SecretaryWebSocketFactory = (url, credential) => {
			expect(url).toContain(`after_seq=${sockets.length === 0 ? 2 : 3}`);
			expect(credential).toBe("cli_pi_only");
			const socket = new FakeSocket();
			sockets.push(socket);
			return socket;
		};
		const received: string[] = [];
		const subscription = await client(fetch, factory).subscribeConversation({
			onEntry: (entry) => {
				received.push(entry.id);
			},
		});
		expect(received).toEqual(["e1", "e2"]);
		sockets[0]!.emit({ id: "e2", seq: 2 });
		sockets[0]!.emit({ id: "e3", seq: 3 });
		await new Promise((resolve) => setTimeout(resolve, 0));
		expect(received).toEqual(["e1", "e2", "e3"]);
		sockets[0]!.close();
		await new Promise((resolve) => setTimeout(resolve, 25));
		expect(sockets).toHaveLength(2);
		sockets[1]!.emit({ id: "e3", seq: 3 });
		await new Promise((resolve) => setTimeout(resolve, 0));
		expect(received).toEqual(["e1", "e2", "e3"]);
		expect(calls[0]!.input.toString()).toContain("after_seq=0");
		subscription.close();
	});

	test("reconnect reuses server state and selected worker, without a spawn or Node protocol call", async () => {
		const { fetch, calls } = fetchSequence([
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
			response({ worker: { worker_ref: "worker-7", status: "working" }, turns: [] }),
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
			response({ worker: { worker_ref: "worker-7", status: "idle" }, turns: [] }),
		]);
		const secretary = client(fetch);
		secretary.selectWorker("worker-7");
		await secretary.reconnect();
		const details = await secretary.getWorker("worker-7");
		expect(details.worker.worker_ref).toBe("worker-7");
		await secretary.reconnect();
		expect(secretary.selectedWorkerRef).toBe("worker-7");
		expect(calls.map(({ input }) => input.toString()).some((url) => url.includes("/spawn"))).toBe(false);
		expect(
			calls
				.map(({ input }) => input.toString())
				.some((url) => url.includes("node/ws") || url.includes("nodes/connect")),
		).toBe(false);
	});

	test("resolves needs_input and approval using only the server request_id", async () => {
		const { fetch, calls } = fetchSequence([
			response({ worker: { worker_ref: "worker-7", status: "needs_input" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7", status: "working" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7", status: "working" }, turns: [] }),
		]);
		const secretary = client(fetch);
		await secretary.messageWorker("worker-7", "input answer", { requestId: "request-1", idempotencyKey: "input-1" });
		await secretary.respondWorker("worker-7", "request-1", "approved", { idempotencyKey: "approval-1" });
		await secretary.approve("request-2", { idempotencyKey: "approval-route-1" });
		expect(calls.map(({ input }) => input.toString())).toEqual([
			`${baseUrl}/v1/workers/worker-7/message`,
			`${baseUrl}/v1/workers/worker-7/message`,
			`${baseUrl}/v1/approvals/request-2/approve`,
		]);
		expect(JSON.parse(String(calls[0]!.init?.body))).toMatchObject({ request_id: "request-1", text: "input answer" });
		expect(JSON.parse(String(calls[1]!.init?.body))).toEqual({ request_id: "request-1", text: "approved" });
		expect(JSON.parse(String(calls[2]!.init?.body))).toEqual({});
	});

	test("uses the existing Worker and Approval endpoints with idempotency", async () => {
		const { fetch, calls } = fetchSequence([
			response({ worker: { worker_ref: "worker-7" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7" }, turns: [] }),
			response({ worker: { worker_ref: "worker-7" }, turns: [] }),
		]);
		const secretary = client(fetch);
		await secretary.messageWorker("worker-7", "answer", { requestId: "input-1", idempotencyKey: "message-1" });
		await secretary.cancelWorker("worker-7", { idempotencyKey: "cancel-1" });
		await secretary.closeWorker("worker-7", { idempotencyKey: "close-1" });
		await secretary.approve("approval-1", { idempotencyKey: "approve-1" });
		await secretary.deny("approval-1", { idempotencyKey: "deny-1" });
		expect(calls.map(({ input }) => input.toString())).toEqual([
			`${baseUrl}/v1/workers/worker-7/message`,
			`${baseUrl}/v1/workers/worker-7/cancel`,
			`${baseUrl}/v1/workers/worker-7/close`,
			`${baseUrl}/v1/approvals/approval-1/approve`,
			`${baseUrl}/v1/approvals/approval-1/deny`,
		]);
		expect((calls[0]!.init?.headers as Headers).get("Idempotency-Key")).toBe("message-1");
		expect((calls[3]!.init?.headers as Headers).get("Idempotency-Key")).toBe("approve-1");
		expect(JSON.parse(String(calls[0]!.init?.body))).toMatchObject({ text: "answer", request_id: "input-1" });
	});

	test("does not hide authentication failures or expose a Node credential", async () => {
		const { fetch } = fetchSequence([response({ error: "no" }, 401), response({ error: "no" }, 401)]);
		await expect(client(fetch).listWorkers()).rejects.toEqual(expect.any(SecretaryApiError));
		await expect(client(fetch).listWorkers()).rejects.toHaveProperty("status", 401);
	});
});

describe("SecretaryClient snapshot paging and stream states", () => {
	function streamClient(
		fetch: typeof globalThis.fetch,
		sockets: FakeSocket[],
		extra: Record<string, unknown> = {},
	): SecretaryClient {
		return new SecretaryClient({
			baseUrl,
			credential: "cli_pi_only",
			fetch,
			webSocketFactory: () => {
				const socket = new FakeSocket();
				sockets.push(socket);
				return socket;
			},
			reconnectInitialMs: 10,
			reconnectMaxMs: 10,
			...extra,
		});
	}

	test("listTail reads the bounded server tail", async () => {
		const { fetch, calls } = fetchSequence([
			response({ entries: [{ id: "e9", seq: 9 }], next_before_seq: 9, next_after_seq: null }),
		]);
		const page = await client(fetch).listTail(50);
		expect(calls[0]!.input.toString()).toContain("/v1/conversation?limit=50");
		expect(page.entries).toHaveLength(1);
		expect(page.next_before_seq).toBe(9);
		expect(page.next_after_seq).toBeNull();
	});

	test("listConversation pages forward to the live cursor", async () => {
		const first = Array.from({ length: 500 }, (_, index) => ({ id: `e${index + 1}`, seq: index + 1 }));
		const { fetch, calls } = fetchSequence([
			response({ entries: first, next_before_seq: null, next_after_seq: 500 }),
			response({ entries: [{ id: "e501", seq: 501 }], next_before_seq: null, next_after_seq: 501 }),
		]);
		const entries = await client(fetch).listConversation(0);
		expect(entries).toHaveLength(501);
		expect(calls.map(({ input }) => input.toString())).toEqual([
			`${baseUrl}/v1/conversation?after_seq=0&limit=500`,
			`${baseUrl}/v1/conversation?after_seq=500&limit=500`,
		]);
	});

	test("revoked close stops reconnect", async () => {
		const { fetch } = fetchSequence([response({ entries: [], next_before_seq: null, next_after_seq: null })]);
		const sockets: FakeSocket[] = [];
		const errors: Error[] = [];
		const subscription = await streamClient(fetch, sockets).subscribeConversation({
			onEntry: () => {},
			onError: (error) => {
				errors.push(error);
			},
		});
		expect(sockets).toHaveLength(1);
		sockets[0]!.close(1008, "Client revoked");
		await new Promise((resolve) => setTimeout(resolve, 30));
		expect(sockets).toHaveLength(1);
		expect(errors.some((error) => error.name === "SecretaryRevokedError")).toBe(true);
		subscription.close();
	});

	test("sequence jump reports one gap without rendering jumped entries", async () => {
		const { fetch } = fetchSequence([response({ entries: [], next_before_seq: null, next_after_seq: null })]);
		const sockets: FakeSocket[] = [];
		const gaps: Array<{ from: number; to: number }> = [];
		const received: string[] = [];
		const subscription = await streamClient(fetch, sockets).subscribeConversation({
			onEntry: (entry) => {
				received.push(entry.id);
			},
			onGap: (gap) => {
				gaps.push({ from: gap.from, to: gap.to });
			},
		});
		sockets[0]!.emit({ id: "e1", seq: 1 });
		sockets[0]!.emit({ id: "e5", seq: 5 });
		sockets[0]!.emit({ id: "e6", seq: 6 });
		await new Promise((resolve) => setTimeout(resolve, 0));
		expect(received).toEqual(["e1"]);
		expect(gaps).toEqual([{ from: 1, to: 5 }]);
		subscription.close();
	});

	test("slow-subscriber close stays reconnectable", async () => {
		const { fetch } = fetchSequence([
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
		]);
		const sockets: FakeSocket[] = [];
		const errors: Error[] = [];
		const subscription = await streamClient(fetch, sockets).subscribeConversation({
			onEntry: () => {},
			onError: (error) => {
				errors.push(error);
			},
		});
		expect(sockets).toHaveLength(1);
		sockets[0]!.close(1008, "subscriber too slow; reconnect with after_seq");
		await new Promise((resolve) => setTimeout(resolve, 50));
		expect(sockets).toHaveLength(2);
		expect(errors.some((error) => error.name === "SecretaryRevokedError")).toBe(false);
		subscription.close();
	});

	test("one socket attempt counts one failure", async () => {
		const { fetch } = fetchSequence([
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
			response({ entries: [], next_before_seq: null, next_after_seq: null }),
		]);
		const sockets: FakeSocket[] = [];
		let offline = 0;
		const subscription = await streamClient(fetch, sockets).subscribeConversation({
			onEntry: () => {},
			offlineAfterFailures: 2,
			onOffline: () => {
				offline += 1;
			},
		});
		expect(sockets).toHaveLength(1);
		sockets[0]!.fail(new Error("network drop"));
		sockets[0]!.close(1000, "");
		await new Promise((resolve) => setTimeout(resolve, 50));
		expect(sockets).toHaveLength(2);
		expect(offline).toBe(0);
		subscription.close();
	});

	test("repeated failures report offline and recovery", async () => {
		let failures = 0;
		const fetch = vi.fn(async () => {
			failures += 1;
			if (failures <= 2) throw new Error("network down");
			return response({ entries: [], next_before_seq: null, next_after_seq: null });
		}) as unknown as typeof globalThis.fetch;
		const sockets: FakeSocket[] = [];
		let lost = 0;
		let offline = 0;
		let recovered = 0;
		const subscription = await streamClient(fetch, sockets).subscribeConversation({
			onEntry: () => {},
			onConnectionLost: () => {
				lost += 1;
			},
			onOffline: () => {
				offline += 1;
			},
			offlineAfterFailures: 2,
			onRecovered: () => {
				recovered += 1;
			},
		});
		await new Promise((resolve) => setTimeout(resolve, 100));
		expect(lost).toBe(1);
		expect(offline).toBe(1);
		sockets[0]!.onopen?.();
		expect(recovered).toBe(1);
		expect(sockets).toHaveLength(1);
		subscription.close();
	});
});
