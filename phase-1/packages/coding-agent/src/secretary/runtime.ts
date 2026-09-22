import {
	SecretaryClient,
	type SecretaryClientIdentity,
	type SecretaryClientOptions,
	type SecretaryPairOptions,
} from "./client.ts";
import {
	SecretaryPresentation,
	type SecretaryPresentationOptions,
	type SecretaryPresentationState,
} from "./presentation.ts";

export interface SecretaryClientRuntimeOptions extends SecretaryPresentationOptions {
	/** Use an already constructed Client, for hosts that own credential loading. */
	readonly client?: SecretaryClient;
	/** Client credential and HTTP/WebSocket transport options. */
	readonly clientOptions?: SecretaryClientOptions;
	/** Owner-approved pairing options. Pairing is performed before the runtime starts. */
	readonly pairOptions?: SecretaryPairOptions;
	/** Open this server-owned Worker observer as part of startup. */
	readonly workerRef?: string;
	/** Keep ordered live subscriptions enabled. Defaults to true. */
	readonly live?: boolean;
}

/**
 * Production lifecycle for the Secretary client view.
 *
 * It composes the existing SecretaryClient adapter and Pi presentation state.
 * It never starts a Worker, creates a Node, or opens the Node protocol.
 */
export class SecretaryClientRuntime {
	readonly client: SecretaryClient;
	readonly presentation: SecretaryPresentation;
	readonly identity: SecretaryClientIdentity | undefined;
	readonly #workerRef: string | undefined;
	readonly #live: boolean;
	#started = false;
	#disposed = false;

	constructor(options: SecretaryClientRuntimeOptions, identity?: SecretaryClientIdentity) {
		if (options.pairOptions !== undefined) {
			throw new TypeError("Use SecretaryClientRuntime.create() for pairing");
		}
		if (options.client && options.clientOptions) throw new TypeError("Provide client or clientOptions, not both");
		if (!options.client && !options.clientOptions)
			throw new TypeError("Provide client, clientOptions, or pairOptions");
		this.client = options.client ?? new SecretaryClient(options.clientOptions!);
		this.identity = identity;
		this.presentation = new SecretaryPresentation(this.client, options);
		this.#workerRef = options.workerRef;
		this.#live = options.live !== false;
	}

	static async create(options: SecretaryClientRuntimeOptions): Promise<SecretaryClientRuntime> {
		if (options.pairOptions === undefined) return new SecretaryClientRuntime(options);
		const paired = await SecretaryClient.pair(options.pairOptions);
		return new SecretaryClientRuntime({ ...options, pairOptions: undefined, client: paired.client }, paired.identity);
	}

	async start(): Promise<SecretaryPresentationState> {
		if (this.#disposed) throw new Error("Secretary Client runtime is disposed");
		if (this.#started) return this.presentation.state;
		await this.presentation.start({ live: this.#live });
		if (this.#workerRef !== undefined) await this.presentation.openWorker(this.#workerRef);
		this.#started = true;
		return this.presentation.state;
	}

	/** Re-read server state while keeping the current Worker observer and cursor. */
	async reconnect(): Promise<SecretaryPresentationState> {
		if (!this.#started) await this.start();
		const workerRef = this.presentation.state.selectedWorkerRef ?? this.#workerRef;
		await this.presentation.dispose();
		this.#started = false;
		const state = await this.presentation.start({ live: this.#live });
		if (workerRef !== undefined) await this.presentation.openWorker(workerRef);
		this.#started = true;
		return state;
	}

	async dispose(): Promise<void> {
		if (this.#disposed) return;
		this.#disposed = true;
		await this.presentation.dispose();
		await this.client.dispose();
	}
}

export async function openSecretaryClientRuntime(
	options: SecretaryClientRuntimeOptions,
): Promise<SecretaryClientRuntime> {
	return SecretaryClientRuntime.create(options);
}
