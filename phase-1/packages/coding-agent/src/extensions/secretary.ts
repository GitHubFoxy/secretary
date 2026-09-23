import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { readFile, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { SecretaryClientRuntime } from "../secretary/runtime.ts";
import type { SecretaryPresentationState } from "../secretary/presentation.ts";

const scopes = ["conversation:read", "conversation:write", "worker:read", "worker:message", "approval:read"] as const;

function safeLine(value: unknown): string | undefined {
	if (typeof value !== "string") return undefined;
	return value.replace(/[\u0000-\u001f\u007f]/g, " ").slice(0, 500);
}

export function workerPickerOptions(workers: SecretaryPresentationState["workers"]): { label: string; workerRef: string }[] {
	const openWorkers = workers.filter((worker) => !worker.archived && worker.status !== "closed");
	const names = openWorkers.map((worker) => safeLine(worker.title)?.trim().slice(0, 60) || worker.worker_ref);
	const counts = new Map<string, number>();
	for (const name of names) counts.set(name, (counts.get(name) ?? 0) + 1);
	const seen = new Map<string, number>();
	return openWorkers.map((worker, index) => {
		const name = names[index];
		const occurrence = (seen.get(name) ?? 0) + 1;
		seen.set(name, occurrence);
		return {
			label: `Worker: ${name}${(counts.get(name) ?? 0) > 1 ? ` (${occurrence})` : ""}`,
			workerRef: worker.worker_ref,
		};
	});
}

export function summary(state: SecretaryPresentationState): string {
	const lines = ["Secretary server: удалённый чат; ввод ниже пойдёт в локальный Pi", `Secretary: ${state.connection}`];
	for (const entry of state.conversation.slice(-10)) {
		const body = safeLine(entry.body);
		if (body) lines.push(body);
	}
	const workerStatus = state.selectedWorker?.worker.status;
	if (["queued", "starting", "working", "waiting_approval", "needs_input", "offline", "idle", "closed"].includes(workerStatus ?? "")) {
		lines.push(`Worker status: ${workerStatus}`);
	}
	for (const activity of state.workerActivity.slice(-12)) {
		const event = activity as Record<string, unknown>;
		if (event.kind !== "attempt.activity" || typeof event.payload !== "object" || event.payload === null) continue;
		const payload = event.payload as Record<string, unknown>;
		const tool = typeof payload.tool === "string" && /^[a-zA-Z0-9_.-]{1,80}$/.test(payload.tool) ? payload.tool : undefined;
		if (tool && payload.kind === "tool_call") lines.push(`Tool started: ${tool}`);
		if (tool && payload.kind === "tool_result") lines.push(`Tool finished: ${tool}`);
	}
	return lines.join("\n");
}

export default function (pi: ExtensionAPI) {
	let activeRuntime: SecretaryClientRuntime | undefined;
	pi.on("session_shutdown", async () => {
		await activeRuntime?.dispose();
		activeRuntime = undefined;
	});
	pi.registerCommand("secretary", {
		description: "Открыть Secretary и отправить сообщение напрямую серверу",
		handler: async (_args, ctx) => {
			if (!ctx.hasUI) {
				ctx.ui.notify("Secretary требует интерактивный интерфейс Pi", "warning");
				return;
			}
			const baseUrl = process.env.SECRETARY_BASE_URL;
			const credentialFile = process.env.SECRETARY_CLIENT_CREDENTIAL_FILE ?? join(homedir(), ".config/secretary/viewer-credential");
			if (!baseUrl) {
				ctx.ui.notify("Задайте SECRETARY_BASE_URL", "error");
				return;
			}
			let credential: string;
			try {
				if ((await stat(credentialFile)).mode & 0o077) throw new Error("credential file must be mode 0600");
				credential = (await readFile(credentialFile, "utf8")).trim();
				if (!credential) throw new Error("credential file is empty");
			} catch {
				ctx.ui.notify(`Не удалось прочитать credential из ${credentialFile}; нужен файл с правами 0600`, "error");
				return;
			}
			const previousRuntime = activeRuntime;
			activeRuntime = undefined;
			await previousRuntime?.dispose();
			ctx.ui.setWidget("secretary", undefined);
			let showRemoteView = false;
			const runtime = new SecretaryClientRuntime({
				clientOptions: { baseUrl, credential },
				live: true,
				onChange: (state) => {
					if (showRemoteView) ctx.ui.setWidget("secretary", [summary(state)]);
				},
			});
			activeRuntime = runtime;
			const discardRuntime = async () => {
				showRemoteView = false;
				if (activeRuntime === runtime) activeRuntime = undefined;
				await runtime.dispose();
				ctx.ui.setWidget("secretary", undefined);
			};
			try {
				await runtime.start();
				const workerOptions = workerPickerOptions(runtime.presentation.state.workers);
				const choice = await ctx.ui.select("Кому отправить сообщение?", ["Secretary", ...workerOptions.map((option) => option.label)]);
				if (!choice) {
					await discardRuntime();
					ctx.ui.notify("Выбор отменён. Обычный ввод Pi остаётся локальным.", "info");
					return;
				}
				showRemoteView = true;
				ctx.ui.setWidget("secretary", [summary(runtime.presentation.state)]);
				let workerRef: string | undefined;
				if (choice !== "Secretary") {
					workerRef = workerOptions.find((option) => option.label === choice)?.workerRef;
					if (!workerRef) {
						await discardRuntime();
						return;
					}
					await runtime.presentation.openWorker(workerRef);
				}
				const text = await ctx.ui.editor("Сообщение отправится Secretary server напрямую. Обычный ввод Pi остаётся локальным.");
				if (!text?.trim()) {
					await discardRuntime();
					ctx.ui.notify("Отправка отменена. Обычный ввод Pi остаётся локальным.", "info");
					return;
				}
				if (workerRef) await runtime.presentation.messageWorker(text);
				else await runtime.presentation.sendMessage(text);
				ctx.ui.notify("Сообщение отправлено в Secretary. Обычный ввод Pi ниже остаётся локальным.", "info");
			} catch {
				await discardRuntime();
				ctx.ui.notify("Не удалось связаться с Secretary server", "error");
			}
		},
	});
}

export { scopes as secretaryPairScopes };
