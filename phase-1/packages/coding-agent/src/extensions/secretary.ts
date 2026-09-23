import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { SecretaryClientRuntime } from "../secretary/runtime.ts";
import type { SecretaryPresentationState } from "../secretary/presentation.ts";

const scopes = ["conversation:read", "conversation:write", "worker:read", "worker:message", "approval:read"] as const;

function safeLine(value: unknown): string | undefined {
	if (typeof value !== "string") return undefined;
	return value.replace(/[\u0000-\u001f\u007f]/g, " ").slice(0, 500);
}

function summary(state: SecretaryPresentationState): string {
	const lines = [`Secretary: ${state.connection}`];
	for (const entry of state.conversation.slice(-10)) {
		const body = safeLine(entry.body);
		if (body) lines.push(body);
	}
	for (const activity of state.workerActivity.slice(-8)) {
		const item = activity as Record<string, unknown>;
		const status = item.kind === "status" ? item.status : undefined;
		if (status === "working" || status === "idle" || status === "needs_input" || status === "completed" || status === "failed") {
			lines.push(`Worker: ${status}`);
		}
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
			const credential = process.env.SECRETARY_CLIENT_CREDENTIAL;
			if (!baseUrl || !credential) {
				ctx.ui.notify("Задайте SECRETARY_BASE_URL и SECRETARY_CLIENT_CREDENTIAL", "error");
				return;
			}
			await activeRuntime?.dispose();
			const runtime = new SecretaryClientRuntime({
				clientOptions: { baseUrl, credential },
				live: true,
				onChange: (state) => ctx.ui.setWidget("secretary", [summary(state)]),
			});
			activeRuntime = runtime;
			try {
				await runtime.start();
				const choice = await ctx.ui.select("Кому отправить сообщение?", ["Secretary", ...runtime.presentation.state.workers.map((worker) => `Worker: ${worker.worker_ref}`)]);
				if (!choice) return;
				let workerRef: string | undefined;
				if (choice !== "Secretary") {
					workerRef = choice.slice("Worker: ".length);
					await runtime.presentation.openWorker(workerRef);
				}
				const text = await ctx.ui.editor("Сообщение отправится напрямую в Secretary server, минуя модель Pi");
				if (!text?.trim()) return;
				if (workerRef) await runtime.presentation.messageWorker(text);
				else await runtime.presentation.sendMessage(text);
				ctx.ui.notify("Сообщение отправлено", "info");
			} catch {
				ctx.ui.notify("Не удалось связаться с Secretary server", "error");
				if (activeRuntime === runtime) activeRuntime = undefined;
				await runtime.dispose();
			}
		},
	});
}

export { scopes as secretaryPairScopes };
