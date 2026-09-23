import { randomUUID } from "node:crypto";

export type SecretaryClientScope =
	| "conversation:read"
	| "conversation:write"
	| "worker:read"
	| "worker:write"
	| "worker:message"
	| "approval:read"
	| "approval:write"
	| "project:read"
	| "node:read";

// Read-only viewer scopes sent when pairing without an explicit scope list.
// The server rejects omitted and empty scope lists, so the Pi client must
// always state its grant explicitly.
export const defaultPairScopes: readonly SecretaryClientScope[] = ["conversation:read", "worker:read", "approval:read"];

export interface SecretaryClientIdentity {
	readonly id: string;
	readonly device_id: string;
	readonly display_name: string;
	readonly platform: string;
	readonly scopes: readonly SecretaryClientScope[];
	readonly status: "pending" | "active" | "revoked";
}

export interface SecretaryPairOptions {
	readonly baseUrl: string;
	readonly bootstrapToken: string;
	readonly deviceId: string;
	readonly displayName: string;
	readonly platform?: string;
	readonly scopes?: readonly SecretaryClientScope[];
	readonly idempotencyKey?: string;
	readonly pollIntervalMs?: number;
	readonly timeoutMs?: number;
	readonly signal?: AbortSignal;
	readonly fetch?: typeof globalThis.fetch;
	readonly webSocketFactory?: SecretaryWebSocketFactory;
}

export interface PairedSecretaryClient {
	readonly client: SecretaryClient;
	readonly identity: SecretaryClientIdentity;
	readonly credential: string;
}

export interface SecretaryClientOptions {
	readonly baseUrl: string;
	readonly credential: string;
	readonly clientId?: string;
	readonly fetch?: typeof globalThis.fetch;
	readonly webSocketFactory?: SecretaryWebSocketFactory;
	readonly reconnectInitialMs?: number;
	readonly reconnectMaxMs?: number;
}

export interface ConversationEntry {
	readonly id: string;
	readonly seq: number;
	readonly conversation_id?: string;
	readonly kind?: string;
	readonly body?: string;
	readonly [key: string]: unknown;
}

export interface SecretaryEvent {
	readonly id?: string;
	readonly seq: number;
	readonly kind: string;
	readonly [key: string]: unknown;
}

export interface Worker {
	readonly worker_ref: string;
	readonly title?: string;
	readonly status: string;
	readonly archived?: boolean;
	readonly [key: string]: unknown;
}

export interface WorkerDetails {
	readonly worker: Worker;
	readonly turns: readonly Record<string, unknown>[];
	readonly attempts?: readonly Record<string, unknown>[];
	readonly outcomes?: readonly Record<string, unknown>[];
	readonly results?: readonly Record<string, unknown>[];
	readonly approvals?: readonly Approval[];
	readonly [key: string]: unknown;
}

export interface WorkerActivity {
	readonly seq: number;
	readonly [key: string]: unknown;
}

export interface Approval {
	readonly id: string;
	readonly kind: string;
	readonly action_summary: string;
	readonly risk_category: string;
	readonly state: "pending" | "approved" | "denied" | "expired" | string;
	readonly requested_at: string;
	readonly expires_at: string | null;
	readonly [key: string]: unknown;
}

export interface ConversationPage {
	readonly entries: readonly ConversationEntry[];
	readonly next_before_seq: number | null;
	readonly next_after_seq: number | null;
}

/** Page size for forward replay collection. Snapshot tails use the server default. */
const conversationReplayLimit = 500;

export interface UserDocument {
	readonly content: string;
	readonly revision: number;
	readonly [key: string]: unknown;
}

export interface Project {
	readonly id: string;
	readonly name: string;
	readonly [key: string]: unknown;
}

export interface NodeInventory {
	readonly node: string;
	readonly online: boolean;
	readonly [key: string]: unknown;
}

export interface MessageAcknowledgement {
	readonly entry: ConversationEntry;
	readonly message_id: string;
	readonly entry_seq: number;
	readonly state: string;
	readonly worker_ref?: string;
	readonly turn_id?: string;
	readonly duplicate: boolean;
}

export interface WorkerSubscription {
	readonly workerRef: string;
	readonly details: WorkerDetails;
	readonly activity: readonly WorkerActivity[];
	readonly subscription: SecretarySubscription;
}

export interface SecretarySubscription {
	readonly close: () => void;
}

export interface SecretaryWebSocket {
	readonly OPEN: number;
	readonly readyState: number;
	onopen: (() => void) | null;
	onmessage: ((event: { readonly data: unknown }) => void) | null;
	onerror: ((event: unknown) => void) | null;
	onclose: ((event: { readonly code: number; readonly reason?: string }) => void) | null;
	close(code?: number, reason?: string): void;
}

export type SecretaryWebSocketFactory = (url: string, credential: string) => SecretaryWebSocket;

export class SecretaryApiError extends Error {
	readonly status: number;
	readonly body: unknown;

	constructor(status: number, body: unknown) {
		super(`Secretary API request failed with HTTP ${status}`);
		this.name = "SecretaryApiError";
		this.status = status;
		this.body = body;
	}
}

interface SequenceSubscriptionOptions<T> {
	readonly replay: (afterSeq: number) => Promise<readonly T[]>;
	readonly socketPath: string;
	readonly sequence: (value: T) => number;
	readonly identity: (value: T) => string | undefined;
	readonly onValue: (value: T) => void | Promise<void>;
	readonly onError?: (error: Error) => void;
	readonly afterSeq?: number;
	/** Fired when a sequence jump proves missed entries; the host must resync. */
	readonly onGap?: (gap: { readonly from: number; readonly to: number }) => void | Promise<void>;
	/** Fired on the first failure after a success; the host shows reconnecting. */
	readonly onConnectionLost?: () => void;
	/** Fired after offlineAfterFailures consecutive failures; the host shows offline. */
	readonly onOffline?: () => void;
	readonly offlineAfterFailures?: number;
	/** Fired on the first success after failures; the host shows connected. */
	readonly onRecovered?: () => void;
}

/** The credential was revoked: reconnect must stop until a new pairing. */
export class SecretaryRevokedError extends Error {
	constructor(message = "Secretary Client credential was revoked") {
		super(message);
		this.name = "SecretaryRevokedError";
	}
}

function isRevokedApiError(error: unknown): boolean {
	return error instanceof SecretaryApiError && (error.status === 401 || error.status === 403);
}

type InternalSubscription = SecretarySubscription;

/**
 * Thin HTTP/WebSocket adapter for Secretary's public Client API.
 *
 * This module deliberately does not import the Go domain package and does not
 * expose a Node transport. Pi presentation and extensions stay local to the
 * coding-agent process; Workers, approvals and replay state stay server-owned.
 */
export class SecretaryClient {
	readonly #baseUrl: string;
	readonly #credential: string;
	readonly #clientId: string | undefined;
	readonly #fetch: typeof globalThis.fetch;
	readonly #webSocketFactory: SecretaryWebSocketFactory;
	readonly #reconnectInitialMs: number;
	readonly #reconnectMaxMs: number;
	readonly #subscriptions = new Set<InternalSubscription>();
	#conversationSeq = 0;
	#selectedWorkerRef: string | undefined;
	#closed = false;

	constructor(options: SecretaryClientOptions) {
		if (options.credential.trim() === "") throw new TypeError("A Client credential is required");
		this.#baseUrl = normalizeBaseUrl(options.baseUrl);
		this.#credential = options.credential;
		this.#clientId = options.clientId;
		this.#fetch = options.fetch ?? globalThis.fetch.bind(globalThis);
		this.#webSocketFactory = options.webSocketFactory ?? defaultWebSocketFactory;
		this.#reconnectInitialMs = Math.max(10, options.reconnectInitialMs ?? 250);
		this.#reconnectMaxMs = Math.max(this.#reconnectInitialMs, options.reconnectMaxMs ?? 5_000);
	}

	static async pair(options: SecretaryPairOptions): Promise<PairedSecretaryClient> {
		const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
		const baseUrl = normalizeBaseUrl(options.baseUrl);
		const idempotencyKey = options.idempotencyKey ?? randomUUID();
		const pairing = await requestJSON<{
			client_id: string;
			pending_token: string;
			id?: string;
			device_id?: string;
			display_name?: string;
			platform?: string;
			scopes?: readonly SecretaryClientScope[];
			status?: string;
		}>(
			fetchImpl,
			baseUrl,
			"/v1/clients/pair",
			{
				method: "POST",
				body: {
					bootstrap_token: options.bootstrapToken,
					device_id: options.deviceId,
					display_name: options.displayName,
					platform: options.platform ?? "pi",
					scopes: options.scopes ?? defaultPairScopes,
					idempotency_key: idempotencyKey,
				},
				signal: options.signal,
			},
			undefined,
		);
		if (!pairing.client_id || !pairing.pending_token)
			throw new Error("Secretary pairing did not return a pending handoff");

		const deadline = Date.now() + Math.max(1, options.timeoutMs ?? 5 * 60_000);
		const pollInterval = Math.max(10, options.pollIntervalMs ?? 500);
		let status: { client_id: string; status: string; credential_ready: boolean; redeemed: boolean };
		for (;;) {
			options.signal?.throwIfAborted();
			status = await requestJSON(
				fetchImpl,
				baseUrl,
				`/v1/clients/${encodeURIComponent(pairing.client_id)}/poll`,
				{ method: "GET", signal: options.signal },
				pairing.pending_token,
			);
			if (status.status === "revoked") throw new Error("Secretary Client pairing was revoked");
			if (status.status === "active" && status.credential_ready && !status.redeemed) break;
			if (Date.now() >= deadline) throw new Error("Timed out waiting for Secretary Client pairing approval");
			await delay(pollInterval, options.signal);
		}

		const redeemed = await requestJSON<{ client_id: string; credential: string; status: "active" }>(
			fetchImpl,
			baseUrl,
			`/v1/clients/${encodeURIComponent(pairing.client_id)}/redeem`,
			{
				method: "POST",
				body: { idempotency_key: `${idempotencyKey}:redeem` },
				signal: options.signal,
			},
			pairing.pending_token,
		);
		if (!redeemed.credential) throw new Error("Secretary pairing did not return a Client credential");
		const identity: SecretaryClientIdentity = {
			id: pairing.id ?? pairing.client_id,
			device_id: pairing.device_id ?? options.deviceId,
			display_name: pairing.display_name ?? options.displayName,
			platform: pairing.platform ?? options.platform ?? "pi",
			scopes: pairing.scopes ?? options.scopes ?? defaultPairScopes,
			status: "active",
		};
		return {
			client: new SecretaryClient({
				baseUrl,
				credential: redeemed.credential,
				clientId: redeemed.client_id,
				fetch: fetchImpl,
				webSocketFactory: options.webSocketFactory,
			}),
			identity,
			credential: redeemed.credential,
		};
	}

	get clientId(): string | undefined {
		return this.#clientId;
	}

	get selectedWorkerRef(): string | undefined {
		return this.#selectedWorkerRef;
	}

	selectWorker(workerRef: string): void {
		if (workerRef.trim() === "") throw new TypeError("worker_ref is required");
		this.#selectedWorkerRef = workerRef;
	}

	async reconnect(signal?: AbortSignal): Promise<void> {
		await this.listConversation(this.#conversationSeq, signal);
	}

	async listConversation(afterSeq = 0, signal?: AbortSignal): Promise<readonly ConversationEntry[]> {
		const collected: ConversationEntry[] = [];
		let cursor = Math.max(0, afterSeq);
		for (;;) {
			const page = await this.#request<ConversationPage>(
				`/v1/conversation?after_seq=${cursor}&limit=${conversationReplayLimit}`,
				{ signal },
			);
			const ordered = [...page.entries].sort((left, right) => left.seq - right.seq);
			collected.push(...ordered);
			if (ordered.length < conversationReplayLimit) break;
			const last = ordered[ordered.length - 1]!.seq;
			if (page.next_after_seq === null || last <= cursor) break;
			cursor = page.next_after_seq;
		}
		if (collected.length > 0)
			this.#conversationSeq = Math.max(this.#conversationSeq, collected[collected.length - 1]!.seq);
		return collected;
	}

	/** Bounded latest tail for snapshots. The server owns limit and cursors. */
	async listTail(limit?: number, signal?: AbortSignal): Promise<ConversationPage> {
		const path = limit === undefined ? "/v1/conversation" : `/v1/conversation?limit=${Math.max(1, limit)}`;
		return this.#request<ConversationPage>(path, { signal });
	}

	async sendMessage(
		body: string,
		options: {
			readonly externalMessageId?: string;
			readonly idempotencyKey?: string;
			readonly signal?: AbortSignal;
		} = {},
	): Promise<MessageAcknowledgement> {
		const text = body.trim();
		if (text === "") throw new TypeError("message body is required");
		const externalMessageId = options.externalMessageId ?? randomUUID();
		return this.#request<MessageAcknowledgement>("/v1/messages", {
			method: "POST",
			body: { external_message_id: externalMessageId, body: text },
			idempotencyKey: options.idempotencyKey ?? externalMessageId,
			signal: options.signal,
		});
	}

	async listWorkers(signal?: AbortSignal): Promise<readonly Worker[]> {
		return this.#request<Worker[]>("/v1/workers", { signal });
	}

	async getUser(signal?: AbortSignal): Promise<UserDocument> {
		return this.#request<UserDocument>("/v1/user", { signal });
	}

	async updateUser(
		content: string,
		expectedRevision: number,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<UserDocument> {
		return this.#request<UserDocument>("/v1/user", {
			method: "PUT",
			body: { content, expected_revision: expectedRevision },
			idempotencyKey: options.idempotencyKey ?? randomUUID(),
			signal: options.signal,
		});
	}

	async listProjects(signal?: AbortSignal): Promise<readonly Project[]> {
		return this.#request<Project[]>("/v1/projects", { signal });
	}

	async listNodes(signal?: AbortSignal): Promise<readonly NodeInventory[]> {
		return this.#request<NodeInventory[]>("/v1/nodes", { signal });
	}

	async getWorker(workerRef: string, signal?: AbortSignal): Promise<WorkerDetails> {
		return this.#request<WorkerDetails>(workerPath(workerRef), { signal });
	}

	async listWorkerTurns(workerRef: string, signal?: AbortSignal): Promise<readonly Record<string, unknown>[]> {
		return this.#request<readonly Record<string, unknown>[]>(`${workerPath(workerRef)}/turns`, { signal });
	}

	async listWorkerActivity(workerRef: string, afterSeq = 0, signal?: AbortSignal): Promise<readonly WorkerActivity[]> {
		return this.#request<readonly WorkerActivity[]>(
			`${workerPath(workerRef)}/activity?after_seq=${Math.max(0, afterSeq)}`,
			{ signal },
		);
	}

	async observeWorker(
		workerRef: string,
		options: {
			readonly afterSeq?: number;
			readonly onActivity: (activity: WorkerActivity) => void | Promise<void>;
			readonly onError?: (error: Error) => void;
			readonly onGap?: (gap: { readonly from: number; readonly to: number }) => void | Promise<void>;
			readonly onConnectionLost?: () => void;
			readonly onOffline?: () => void;
			readonly onRecovered?: () => void;
		},
	): Promise<WorkerSubscription> {
		this.selectWorker(workerRef);
		const details = await this.getWorker(workerRef);
		const activity = [...(await this.listWorkerActivity(workerRef, options.afterSeq ?? 0))].sort(
			(left, right) => left.seq - right.seq,
		);
		const cursor = activity.at(-1)?.seq ?? options.afterSeq ?? 0;
		for (const item of activity) await options.onActivity(item);
		const subscription = await this.#subscribe<WorkerActivity>({
			replay: (afterSeq) => this.listWorkerActivity(workerRef, afterSeq),
			socketPath: `${workerPath(workerRef)}/activity/ws`,
			sequence: (item) => item.seq,
			identity: (item) => (typeof item.id === "string" ? item.id : undefined),
			onValue: options.onActivity,
			onError: options.onError,
			onGap: options.onGap,
			onConnectionLost: options.onConnectionLost,
			onOffline: options.onOffline,
			onRecovered: options.onRecovered,
			afterSeq: cursor,
		});
		return { workerRef, details, activity, subscription };
	}

	async listApprovals(signal?: AbortSignal): Promise<readonly Approval[]> {
		return this.#request<Approval[]>("/v1/approvals", { signal });
	}

	async messageWorker(
		workerRef: string,
		text: string,
		options: { readonly requestId?: string; readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		return this.#workerMutation(workerRef, "message", { text, request_id: options.requestId }, options);
	}

	async respondWorker(
		workerRef: string,
		requestId: string,
		response: string,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		if (requestId.trim() === "") throw new TypeError("request_id is required");
		return this.messageWorker(workerRef, response, { requestId, ...options });
	}

	async cancelWorker(
		workerRef: string,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		return this.#workerMutation(workerRef, "cancel", {}, options);
	}

	async closeWorker(
		workerRef: string,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		return this.#workerMutation(workerRef, "close", {}, options);
	}

	/** Resolve a durable Approval by its server-issued approval id. */
	async approve(
		approvalId: string,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		if (approvalId.trim() === "") throw new TypeError("approval id is required");
		return this.#request<WorkerDetails>(`/v1/approvals/${encodeURIComponent(approvalId)}/approve`, {
			method: "POST",
			body: {},
			idempotencyKey: options.idempotencyKey ?? randomUUID(),
			signal: options.signal,
		});
	}

	/** Resolve an Approval by its server-issued request_id. */
	async deny(
		requestId: string,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal } = {},
	): Promise<WorkerDetails> {
		if (requestId.trim() === "") throw new TypeError("request_id is required");
		return this.#request<WorkerDetails>(`/v1/approvals/${encodeURIComponent(requestId)}/deny`, {
			method: "POST",
			body: {},
			idempotencyKey: options.idempotencyKey ?? randomUUID(),
			signal: options.signal,
		});
	}

	async subscribeConversation(options: {
		readonly afterSeq?: number;
		readonly onEntry: (entry: ConversationEntry) => void | Promise<void>;
		readonly onError?: (error: Error) => void;
		readonly onGap?: (gap: { readonly from: number; readonly to: number }) => void | Promise<void>;
		readonly onConnectionLost?: () => void;
		readonly onOffline?: () => void;
		readonly offlineAfterFailures?: number;
		readonly onRecovered?: () => void;
	}): Promise<SecretarySubscription> {
		return this.#subscribe<ConversationEntry>({
			replay: (afterSeq) => this.listConversation(afterSeq),
			socketPath: "/v1/conversation/ws",
			sequence: (entry) => entry.seq,
			identity: (entry) => entry.id,
			onValue: async (entry) => {
				this.#conversationSeq = Math.max(this.#conversationSeq, entry.seq);
				await options.onEntry(entry);
			},
			onError: options.onError,
			onGap: options.onGap,
			onConnectionLost: options.onConnectionLost,
			onOffline: options.onOffline,
			offlineAfterFailures: options.offlineAfterFailures,
			onRecovered: options.onRecovered,
			afterSeq: options.afterSeq ?? this.#conversationSeq,
		});
	}

	async subscribeSecretaryTurn(
		turnId: string,
		options: {
			readonly afterSeq?: number;
			readonly onEvent: (event: SecretaryEvent) => void | Promise<void>;
			readonly onError?: (error: Error) => void;
			readonly onGap?: (gap: { readonly from: number; readonly to: number }) => void | Promise<void>;
			readonly onConnectionLost?: () => void;
			readonly onOffline?: () => void;
			readonly onRecovered?: () => void;
		},
	): Promise<SecretarySubscription> {
		const path = `/v1/secretary/turns/${encodeURIComponent(turnId)}/stream`;
		return this.#subscribe<SecretaryEvent>({
			replay: async (afterSeq) => {
				const result = await this.#request<{ readonly events: readonly SecretaryEvent[] }>(
					`${path}?after_seq=${Math.max(0, afterSeq)}`,
				);
				return result.events;
			},
			socketPath: `${path}/ws`,
			sequence: (event) => event.seq,
			identity: (event) => event.id,
			onValue: options.onEvent,
			onError: options.onError,
			onGap: options.onGap,
			onConnectionLost: options.onConnectionLost,
			onOffline: options.onOffline,
			onRecovered: options.onRecovered,
			afterSeq: options.afterSeq ?? 0,
		});
	}

	async dispose(): Promise<void> {
		this.#closed = true;
		for (const subscription of [...this.#subscriptions]) subscription.close();
		this.#subscriptions.clear();
	}

	async #workerMutation(
		workerRef: string,
		action: string,
		body: Record<string, unknown>,
		options: { readonly idempotencyKey?: string; readonly signal?: AbortSignal },
	): Promise<WorkerDetails> {
		this.selectWorker(workerRef);
		return this.#request<WorkerDetails>(`${workerPath(workerRef)}/${action}`, {
			method: "POST",
			body,
			idempotencyKey: options.idempotencyKey ?? randomUUID(),
			signal: options.signal,
		});
	}

	async #request<T>(
		path: string,
		options: {
			readonly method?: string;
			readonly body?: unknown;
			readonly idempotencyKey?: string;
			readonly signal?: AbortSignal;
		} = {},
	): Promise<T> {
		if (this.#closed) throw new Error("Secretary Client is disposed");
		return requestJSON<T>(this.#fetch, this.#baseUrl, path, options, this.#credential);
	}

	async #subscribe<T>(options: SequenceSubscriptionOptions<T>): Promise<SecretarySubscription> {
		let cursor = options.afterSeq ?? 0;
		let socket: SecretaryWebSocket | undefined;
		let timer: ReturnType<typeof setTimeout> | undefined;
		let closed = false;
		let reconnectMs = this.#reconnectInitialMs;
		let failures = 0;
		let gapNotified = false;
		const offlineAfter = Math.max(1, options.offlineAfterFailures ?? 5);
		const seen = new Set<string>();
		const delivered = (item: T): boolean => {
			const seq = options.sequence(item);
			const identity = options.identity(item);
			if (seq <= cursor || (identity !== undefined && seen.has(identity))) return false;
			if (cursor > 0 && seq > cursor + 1) {
				if (!gapNotified) {
					gapNotified = true;
					void Promise.resolve(options.onGap?.({ from: cursor, to: seq })).catch((error: unknown) =>
						options.onError?.(toError(error)),
					);
				}
				return false;
			}
			if (identity !== undefined) seen.add(identity);
			cursor = Math.max(cursor, seq);
			void Promise.resolve(options.onValue(item)).catch((error: unknown) => options.onError?.(toError(error)));
			return true;
		};
		const deliver = (items: readonly T[]): void => {
			for (const item of [...items].sort((left, right) => options.sequence(left) - options.sequence(right)))
				delivered(item);
		};
		const stopRevoked = (error: unknown): void => {
			if (closed || this.#closed) return;
			closed = true;
			if (timer !== undefined) {
				clearTimeout(timer);
				timer = undefined;
			}
			if (socket !== undefined) {
				socket.onclose = null;
				socket.onerror = null;
				socket.close(1000, "Credential revoked");
			}
			options.onError?.(error instanceof Error ? error : new SecretaryRevokedError());
		};
		const noteSuccess = (): void => {
			if (failures === 0) return;
			failures = 0;
			reconnectMs = this.#reconnectInitialMs;
			options.onRecovered?.();
		};
		const noteFailure = (): void => {
			failures += 1;
			if (failures === 1) options.onConnectionLost?.();
			if (failures === offlineAfter) options.onOffline?.();
		};
		const schedule = (error?: unknown): void => {
			if (closed || this.#closed) return;
			if (error !== undefined && isRevokedApiError(error)) {
				stopRevoked(error);
				return;
			}
			if (timer !== undefined) return;
			if (error !== undefined) options.onError?.(toError(error));
			noteFailure();
			const wait = reconnectMs;
			reconnectMs = Math.min(this.#reconnectMaxMs, reconnectMs * 2);
			timer = setTimeout(() => {
				timer = undefined;
				void connect();
			}, wait);
		};
		const connect = async (): Promise<void> => {
			if (closed || this.#closed) return;
			try {
				deliver(await options.replay(cursor));
				if (closed || this.#closed) return;
				const url = websocketUrl(this.#baseUrl, options.socketPath, cursor);
				socket = this.#webSocketFactory(url, this.#credential);
				socket.onopen = () => {
					noteSuccess();
				};
				socket.onmessage = (event) => {
					void parseSocketValue<T>(event.data)
						.then((value) => {
							if (!closed) delivered(value);
						})
						.catch(schedule);
				};
				socket.onerror = (event) => schedule(event);
				socket.onclose = (event) => {
					if (event?.code === 1008 && event?.reason === "Client revoked") {
						stopRevoked(new SecretaryRevokedError());
						return;
					}
					schedule();
				};
			} catch (error) {
				schedule(error);
			}
		};
		await connect();
		const subscription: InternalSubscription = {
			close: () => {
				if (closed) return;
				closed = true;
				if (timer !== undefined) clearTimeout(timer);
				if (socket !== undefined) {
					socket.onclose = null;
					socket.onerror = null;
					socket.close(1000, "Client closed");
				}
				this.#subscriptions.delete(subscription);
			},
		};
		this.#subscriptions.add(subscription);
		return subscription;
	}
}

export async function pairSecretaryClient(options: SecretaryPairOptions): Promise<PairedSecretaryClient> {
	return SecretaryClient.pair(options);
}

async function requestJSON<T>(
	fetchImpl: typeof globalThis.fetch,
	baseUrl: string,
	path: string,
	options: {
		readonly method?: string;
		readonly body?: unknown;
		readonly idempotencyKey?: string;
		readonly signal?: AbortSignal;
	},
	credential?: string,
): Promise<T> {
	const headers = new Headers({ Accept: "application/json" });
	if (options.body !== undefined) headers.set("Content-Type", "application/json");
	if (credential !== undefined) headers.set("Authorization", `Bearer ${credential}`);
	if (options.idempotencyKey !== undefined) headers.set("Idempotency-Key", options.idempotencyKey);
	const response = await fetchImpl(new URL(path, baseUrl), {
		method: options.method ?? "GET",
		headers,
		body: options.body === undefined ? undefined : JSON.stringify(options.body),
		signal: options.signal,
	});
	const text = await response.text();
	let value: unknown;
	if (text !== "") {
		try {
			value = JSON.parse(text);
		} catch {
			value = text;
		}
	}
	if (!response.ok) throw new SecretaryApiError(response.status, value);
	return value as T;
}

function normalizeBaseUrl(value: string): string {
	const url = new URL(value);
	if (url.protocol !== "http:" && url.protocol !== "https:")
		throw new TypeError("Secretary baseUrl must use HTTP or HTTPS");
	return `${url.toString().replace(/\/$/, "")}/`;
}

function workerPath(workerRef: string): string {
	if (workerRef.trim() === "") throw new TypeError("worker_ref is required");
	return `/v1/workers/${encodeURIComponent(workerRef)}`;
}

function websocketUrl(baseUrl: string, path: string, afterSeq: number): string {
	const url = new URL(path, baseUrl);
	url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
	url.searchParams.set("after_seq", String(Math.max(0, afterSeq)));
	return url.toString();
}

function defaultWebSocketFactory(url: string, credential: string): SecretaryWebSocket {
	return new globalThis.WebSocket(url, ["secretary.v1", `secretary.bearer.${credential}`]) as unknown as SecretaryWebSocket;
}

async function parseSocketValue<T>(data: unknown): Promise<T> {
	if (typeof data === "string") return JSON.parse(data) as T;
	if (data instanceof ArrayBuffer) return JSON.parse(new TextDecoder().decode(data)) as T;
	if (data instanceof Blob) return JSON.parse(await data.text()) as T;
	throw new Error("Secretary WebSocket delivered an unsupported frame");
}

async function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
	await new Promise<void>((resolve, reject) => {
		const timer = setTimeout(resolve, milliseconds);
		if (signal === undefined) return;
		const abort = () => {
			clearTimeout(timer);
			reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
		};
		if (signal.aborted) abort();
		else signal.addEventListener("abort", abort, { once: true });
	});
}

function toError(error: unknown): Error {
	return error instanceof Error ? error : new Error(String(error));
}
