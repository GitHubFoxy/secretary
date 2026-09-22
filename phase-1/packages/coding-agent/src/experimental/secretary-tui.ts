import { type Component, Container, Text } from "@earendil-works/pi-tui";
import type { SecretaryCommand } from "../cli/experimental/commands/secretary.ts";
import { getAgentDir } from "../config.ts";
import { SettingsManager } from "../core/settings-manager.ts";
import { createInteractiveTui } from "../modes/interactive/tui-renderer.ts";
import type { SecretaryPresentationState } from "../secretary/presentation.ts";
import { openSecretaryClientRuntime } from "../secretary/runtime.ts";
import { applySecretaryAction, runtimeOptionsFromCommand } from "./secretary-client.ts";

class SecretaryView implements Component {
	readonly #content = new Container();
	readonly #requestRender: () => void;
	readonly #finish: () => void;

	constructor(requestRender: () => void, finish: () => void) {
		this.#requestRender = requestRender;
		this.#finish = finish;
	}

	update(state: SecretaryPresentationState): void {
		this.#content.clear();
		this.#content.addChild(new Text(`Secretary Client (${state.connection})`, 1, 0));
		this.#content.addChild(new Text("Personal Conversation", 1, 0));
		for (const entry of state.conversation) {
			this.#content.addChild(new Text(`[${entry.seq}] ${entry.body ?? JSON.stringify(entry)}`, 1, 0));
		}
		this.#content.addChild(new Text("Workers", 1, 0));
		for (const worker of state.workers)
			this.#content.addChild(new Text(`${worker.worker_ref}: ${worker.status}`, 1, 0));
		if (state.selectedWorkerRef !== undefined) {
			this.#content.addChild(new Text(`Observer: ${state.selectedWorkerRef}`, 1, 0));
			for (const activity of state.workerActivity)
				this.#content.addChild(new Text(`[activity ${activity.seq}] ${JSON.stringify(activity)}`, 1, 0));
		}
		for (const approval of state.approvals)
			this.#content.addChild(new Text(`Approval ${approval.id}: ${approval.state}`, 1, 0));
		this.#requestRender();
	}

	render(width: number): string[] {
		return this.#content.render(width);
	}

	handleInput(data: string): void {
		if (data === "\u0003" || data === "\u001b") this.#finish();
	}

	invalidate(): void {
		this.#content.invalidate();
	}

	dispose(): void {
		this.#content.clear();
	}
}

/** Run the Secretary presentation in the same fullscreen TUI host as Pi. */
export async function runSecretaryClientTui(command: SecretaryCommand): Promise<void> {
	const settings = SettingsManager.create(process.cwd(), getAgentDir());
	const tui = createInteractiveTui({
		tuiMode: "fullscreen",
		showHardwareCursor: settings.getShowHardwareCursor(),
		logDirectory: getAgentDir(),
	});
	let view!: SecretaryView;
	let stopped = false;
	let finish!: () => void;
	const finished = new Promise<void>((resolve) => {
		finish = () => {
			if (stopped) return;
			stopped = true;
			tui.stop();
			resolve();
		};
	});
	let runtime: Awaited<ReturnType<typeof openSecretaryClientRuntime>>;
	runtime = await openSecretaryClientRuntime({
		...(await runtimeOptionsFromCommand(command)),
		onChange: (state) => view?.update(state),
		onError: (error) => {
			if (view) view.update(runtime.presentation.state);
			else console.error(error);
		},
	});
	try {
		view = new SecretaryView(() => tui.requestRender(), finish);
		await runtime.start();
		await applySecretaryAction(runtime, command);
		tui.addChild(view);
		tui.setLayoutRoot(view);
		tui.setFocus(view);
		tui.start();
		await finished;
	} finally {
		if (!stopped) tui.stop();
		await runtime.dispose();
	}
}
