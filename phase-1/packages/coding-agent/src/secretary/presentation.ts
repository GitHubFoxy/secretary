import {
	SecretaryRevokedError,
	type Approval,
	type ConversationEntry,
	type SecretaryClient,
	type SecretaryEvent,
	type SecretarySubscription,
	type Worker,
	type WorkerActivity,
	type WorkerDetails,
} from "./client.ts";

export interface SecretaryPresentationState {
	readonly conversation: readonly ConversationEntry[];
	readonly workers: readonly Worker[];
	readonly approvals: readonly Approval[];
	readonly selectedWorkerRef?: string;
	readonly selectedWorker?: WorkerDetails;
	readonly workerActivity: readonly WorkerActivity[];
	readonly secretaryEvents: readonly SecretaryEvent[];
	readonly connection: "starting" | "connected" | "reconnecting" | "offline" | "revoked";
	/** Last successful snapshot read, for the offline screen. */
	readonly snapshotAt?: string;
}

export interface SecretaryPresentationOptions {
	readonly onChange?: (state: SecretaryPresentationState) => void;
	readonly onError?: (error: Error) => void;
}

/**
 * Presentation state for a Pi Secretary view. It delegates every operation to
 * SecretaryClient and never creates or owns a Worker. Existing Pi extensions
 * can render this state alongside their normal presentation facets.
 */
export class SecretaryPresentation {
	readonly #client: SecretaryClient;
	readonly #onChange: (state: SecretaryPresentationState) => void;
	readonly #onError: (error: Error) => void;
	#state: SecretaryPresentationState = {
		conversation: [],
		workers: [],
		approvals: [],
		workerActivity: [],
		secretaryEvents: [],
		connection: "starting",
	};
	#conversationSubscription: SecretarySubscription | undefined;
	#workerSubscription: SecretarySubscription | undefined;
	#secretarySubscription: SecretarySubscription | undefined;

	constructor(client: SecretaryClient, options: SecretaryPresentationOptions = {}) {
		this.#client = client;
		this.#onChange = options.onChange ?? (() => {});
		this.#onError = options.onError ?? (() => {});
	}

	get state(): SecretaryPresentationState {
		return this.#state;
	}

	async start(options: { readonly live?: boolean } = {}): Promise<SecretaryPresentationState> {
		const tail = await this.#client.listTail();
		const [workers, approvals] = await Promise.all([this.#client.listWorkers(), this.#client.listApprovals()]);
		this.#replace({
			conversation: [...tail.entries].sort((left, right) => left.seq - right.seq),
			workers,
			approvals,
			connection: "connected",
			snapshotAt: new Date().toISOString(),
		});
		if (options.live === false) return this.#state;
		this.#conversationSubscription = await this.#client.subscribeConversation({
			afterSeq: this.#lastConversationSeq(),
			onEntry: (entry) => this.#appendConversation(entry),
			onError: (error) => this.#subscriptionError(error),
			onGap: () => this.#resyncAfterGap(),
			onConnectionLost: () => this.#replace({ connection: "reconnecting" }),
			onOffline: () => this.#replace({ connection: "offline" }),
			onRecovered: () => this.#replace({ connection: "connected", snapshotAt: new Date().toISOString() }),
		});
		return this.#state;
	}

	async sendMessage(body: string): Promise<void> {
		const acknowledgement = await this.#client.sendMessage(body);
		if (acknowledgement.turn_id === undefined) return;
		this.#secretarySubscription?.close();
		this.#replace({ secretaryEvents: [] });
		this.#secretarySubscription = await this.#client.subscribeSecretaryTurn(acknowledgement.turn_id, {
			onEvent: (event) => this.#appendSecretaryEvent(event),
			onError: (error) => this.#subscriptionError(error),
			onGap: () => this.#resyncAfterGap(),
			onConnectionLost: () => this.#replace({ connection: "reconnecting" }),
			onOffline: () => this.#replace({ connection: "offline" }),
			onRecovered: () => this.#replace({ connection: "connected", snapshotAt: new Date().toISOString() }),
		});
	}

	async openWorker(workerRef: string): Promise<WorkerDetails> {
		this.#workerSubscription?.close();
		const observed = await this.#client.observeWorker(workerRef, {
			onActivity: (activity) => this.#appendActivity(activity),
			onError: (error) => this.#subscriptionError(error),
			onGap: () => this.#resyncAfterGap(),
			onConnectionLost: () => this.#replace({ connection: "reconnecting" }),
			onOffline: () => this.#replace({ connection: "offline" }),
			onRecovered: () => this.#replace({ connection: "connected", snapshotAt: new Date().toISOString() }),
		});
		this.#replace({
			selectedWorkerRef: workerRef,
			selectedWorker: observed.details,
			workerActivity: observed.activity,
		});
		this.#workerSubscription = observed.subscription;
		return observed.details;
	}

	async messageWorker(text: string, requestId?: string): Promise<WorkerDetails> {
		const workerRef = this.#state.selectedWorkerRef;
		if (workerRef === undefined) throw new Error("No Worker is selected");
		const details = await this.#client.messageWorker(workerRef, text, { requestId });
		this.#replace({ selectedWorker: details });
		return details;
	}

	async respondWorker(requestId: string, response: string): Promise<WorkerDetails> {
		const workerRef = this.#state.selectedWorkerRef;
		if (workerRef === undefined) throw new Error("No Worker is selected");
		const details = await this.#client.respondWorker(workerRef, requestId, response);
		this.#replace({ selectedWorker: details });
		return details;
	}

	async cancelWorker(): Promise<WorkerDetails> {
		const workerRef = this.#state.selectedWorkerRef;
		if (workerRef === undefined) throw new Error("No Worker is selected");
		const details = await this.#client.cancelWorker(workerRef);
		this.#replace({ selectedWorker: details });
		return details;
	}

	async closeWorker(): Promise<WorkerDetails> {
		const workerRef = this.#state.selectedWorkerRef;
		if (workerRef === undefined) throw new Error("No Worker is selected");
		const details = await this.#client.closeWorker(workerRef);
		this.#replace({ selectedWorker: details });
		return details;
	}

	async approve(requestId: string): Promise<WorkerDetails> {
		const workerRef = this.#state.selectedWorkerRef;
		const details =
			workerRef === undefined
				? await this.#client.approve(requestId)
				: await this.#client.respondWorker(workerRef, requestId, "approve");
		this.#replace({ selectedWorker: details });
		return details;
	}

	async deny(approvalId: string): Promise<WorkerDetails> {
		const details = await this.#client.deny(approvalId);
		this.#replace({ selectedWorker: details });
		return details;
	}

	async refresh(): Promise<void> {
		try {
			const [workers, approvals] = await Promise.all([this.#client.listWorkers(), this.#client.listApprovals()]);
			this.#replace({ workers, approvals, connection: "connected", snapshotAt: new Date().toISOString() });
		} catch (error) {
			this.#replace({ connection: "offline" });
			this.#onError(error instanceof Error ? error : new Error(String(error)));
		}
	}

	/** Canonical resync: re-read the snapshot, reset cursors, reopen subscriptions. */
	async resync(): Promise<SecretaryPresentationState> {
		this.#conversationSubscription?.close();
		this.#workerSubscription?.close();
		this.#secretarySubscription?.close();
		this.#conversationSubscription = undefined;
		this.#workerSubscription = undefined;
		this.#secretarySubscription = undefined;
		const workerRef = this.#state.selectedWorkerRef;
		this.#replace({ workerActivity: [], secretaryEvents: [] });
		const state = await this.start({ live: true });
		if (workerRef !== undefined) await this.openWorker(workerRef);
		return state;
	}

	#resyncAfterGap(): void {
		void this.resync().catch((error: unknown) => {
			this.#replace({ connection: "offline" });
			this.#onError(error instanceof Error ? error : new Error(String(error)));
		});
	}

	#subscriptionError(error: Error): void {
		if (error instanceof SecretaryRevokedError) {
			this.#conversationSubscription?.close();
			this.#workerSubscription?.close();
			this.#secretarySubscription?.close();
			this.#conversationSubscription = undefined;
			this.#workerSubscription = undefined;
			this.#secretarySubscription = undefined;
			this.#replace({ connection: "revoked" });
			return;
		}
		this.#onError(error);
	}

	#lastConversationSeq(): number {
		const entries = this.#state.conversation;
		return entries.length === 0 ? 0 : entries[entries.length - 1]!.seq;
	}

	async dispose(): Promise<void> {
		this.#conversationSubscription?.close();
		this.#workerSubscription?.close();
		this.#secretarySubscription?.close();
		this.#conversationSubscription = undefined;
		this.#workerSubscription = undefined;
		this.#secretarySubscription = undefined;
	}

	#appendConversation(entry: ConversationEntry): void {
		if (this.#state.conversation.some((current) => current.id === entry.id)) return;
		this.#replace({ conversation: [...this.#state.conversation, entry].sort((left, right) => left.seq - right.seq) });
	}

	#appendSecretaryEvent(event: SecretaryEvent): void {
		if (this.#state.secretaryEvents.some((current) => current.id !== undefined && current.id === event.id)) return;
		this.#replace({
			secretaryEvents: [...this.#state.secretaryEvents, event].sort((left, right) => left.seq - right.seq),
		});
	}

	#appendActivity(activity: WorkerActivity): void {
		if (this.#state.workerActivity.some((current) => current.seq === activity.seq)) return;
		this.#replace({
			workerActivity: [...this.#state.workerActivity, activity].sort((left, right) => left.seq - right.seq),
		});
	}

	#replace(changes: Partial<SecretaryPresentationState>): void {
		this.#state = { ...this.#state, ...changes };
		this.#onChange(this.#state);
	}
}
