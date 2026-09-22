import { readFile } from "node:fs/promises";
import type { SecretaryCommand } from "../cli/experimental/commands/secretary.ts";
import { SecretaryClientRuntime, type SecretaryClientRuntimeOptions } from "../secretary/runtime.ts";

export interface RunSecretaryClientOptions {
	/** Override the credential source in embedding tests or hosts. */
	readonly runtimeOptions?: Omit<SecretaryClientRuntimeOptions, "clientOptions" | "pairOptions" | "workerRef">;
	/** Keep the process attached to live subscriptions instead of doing one snapshot. */
	readonly waitForLive?: boolean;
}

/** Execute the production Secretary client path used by the experimental CLI. */
export async function runSecretaryClient(
	command: SecretaryCommand,
	options: RunSecretaryClientOptions = {},
): Promise<void> {
	const runtimeOptions = await runtimeOptionsFromCommand(command, {
		...options.runtimeOptions,
		live: options.waitForLive === true && command.once !== true,
	});
	const runtime = await SecretaryClientRuntime.create(runtimeOptions);
	try {
		await runtime.start();
		await applySecretaryAction(runtime, command);
		const state = runtime.presentation.state;
		console.log(
			JSON.stringify({
				conversation: state.conversation,
				workers: state.workers,
				approvals: state.approvals,
				selected_worker_ref: state.selectedWorkerRef,
				worker: state.selectedWorker,
				worker_activity: state.workerActivity,
			}),
		);
		if (command.once === true || options.waitForLive !== true) return;
		await new Promise<void>((resolve) => {
			const finish = (): void => {
				process.off("SIGINT", finish);
				process.off("SIGTERM", finish);
				resolve();
			};
			process.once("SIGINT", finish);
			process.once("SIGTERM", finish);
		});
	} finally {
		await runtime.dispose();
	}
}

export async function applySecretaryAction(runtime: SecretaryClientRuntime, command: SecretaryCommand): Promise<void> {
	if (command.message !== undefined) {
		await runtime.presentation.messageWorker(command.message);
		return;
	}
	if (command.respondRequest !== undefined) {
		if (command.response === undefined) throw new Error("--respond-request requires --response");
		await runtime.presentation.respondWorker(command.respondRequest, command.response);
		return;
	}
	if (command.cancel === true) {
		await runtime.presentation.cancelWorker();
		return;
	}
	if (command.close === true) {
		await runtime.presentation.closeWorker();
		return;
	}
	if (command.approve !== undefined) {
		await runtime.presentation.approve(command.approve);
		return;
	}
	if (command.deny !== undefined) await runtime.presentation.deny(command.deny);
}

export async function runtimeOptionsFromCommand(
	command: SecretaryCommand,
	overrides: Omit<SecretaryClientRuntimeOptions, "clientOptions" | "pairOptions" | "workerRef"> = {},
): Promise<SecretaryClientRuntimeOptions> {
	if (command.bootstrapToken !== undefined) {
		if (command.baseUrl === undefined || command.deviceId === undefined || command.displayName === undefined) {
			throw new Error("Secretary pairing requires --base-url, --device-id, and --display-name");
		}
		return {
			...overrides,
			workerRef: command.workerRef,
			pairOptions: {
				baseUrl: command.baseUrl,
				bootstrapToken: command.bootstrapToken,
				deviceId: command.deviceId,
				displayName: command.displayName,
				platform: command.platform,
			},
		};
	}
	if (overrides.client !== undefined) return { ...overrides, workerRef: command.workerRef };
	if (command.baseUrl === undefined) throw new Error("Secretary client requires --base-url");
	const credential =
		command.credential ??
		(command.credentialFile ? (await readFile(command.credentialFile, "utf8")).trim() : undefined);
	if (!credential) throw new Error("Secretary client credential is empty");
	return {
		...overrides,
		workerRef: command.workerRef,
		clientOptions: { baseUrl: command.baseUrl, credential },
	};
}
