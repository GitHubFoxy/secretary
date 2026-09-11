import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { replicatedState } from "@earendil-works/chord";
import { type AgentHarness, type AgentLane, BACKGROUND_CONTEXT } from "@earendil-works/pi-agent-core";
import { expect, test, vi } from "vitest";
import { activateBuiltinClientServices, openClientRuntime } from "../src/experimental/client-runtime.ts";
import { createExtensionUIBridge, loadFullWorkerResources } from "../src/experimental/full-worker-resources.ts";
import { type RunningServer, startServer } from "../src/experimental/server.ts";
import { ExtensionCommands, type ExtensionUIState } from "../src/experimental/services/extension-commands.ts";
import { CustomEditor } from "../src/modes/interactive/components/custom-editor.ts";
import { getEditorTheme, initTheme } from "../src/modes/interactive/theme/theme.ts";
import { configureExperimentalWorkerModel, createExperimentalSessions } from "./experimental-session-support.ts";

test("worker custom editor forwards text and submits while unhandled Escape bubbles to the client", async () => {
	initTheme("dark");
	const bridge = createExtensionUIBridge("/tmp");
	const state = replicatedState<ExtensionUIState>({
		revision: 0,
		prompt: null,
		statuses: {},
		notifications: [],
		widgets: {},
		workingVisible: true,
	});
	bridge.attachState(state);
	let editor: CustomEditor | undefined;
	const editorFactory = (
		ui: Parameters<NonNullable<ReturnType<typeof bridge.ui.getEditorComponent>>>[0],
		editorTheme: Parameters<NonNullable<ReturnType<typeof bridge.ui.getEditorComponent>>>[1],
		keybindings: Parameters<NonNullable<ReturnType<typeof bridge.ui.getEditorComponent>>>[2],
	) => {
		editor = new CustomEditor(ui, editorTheme, keybindings);
		return editor;
	};
	bridge.ui.setEditorComponent(editorFactory);

	expect(bridge.ui.getEditorComponent()).toBe(editorFactory);
	expect(state.value.editor?.lines).toHaveLength(3);
	expect(await bridge.input("", "ordinary input")).toBe(true);
	expect(state.value.editor?.lines.join("\n")).toContain("ordinary input");
	expect(await bridge.input("", "\r")).toBe(true);
	expect(state.value.editorSubmit).toMatchObject({ id: expect.any(String), text: "ordinary input" });
	expect(await bridge.input("", "\u001b")).toBe(false);

	const onEscape = vi.fn();
	editor!.onEscape = onEscape;
	expect(await bridge.input("", "\u001b")).toBe(true);
	expect(onEscape).toHaveBeenCalledOnce();

	editor!.onEscape = undefined;
	editor!.setAutocompleteProvider({
		getSuggestions: async () => ({
			prefix: "",
			items: [
				{ value: "one", label: "one" },
				{ value: "two", label: "two" },
			],
		}),
		applyCompletion: (lines, cursorLine, cursorCol) => ({ lines, cursorLine, cursorCol }),
	});
	expect(await bridge.input("", "\t")).toBe(true);
	await vi.waitFor(() => expect(editor!.isShowingAutocomplete()).toBe(true));
	expect(await bridge.input("", "\u001b")).toBe(true);
	expect(editor!.isShowingAutocomplete()).toBe(false);
	bridge.dispose();
});

test("worker bridge resolves input from an interactive extension custom component", async () => {
	initTheme("dark");
	const bridge = createExtensionUIBridge("/tmp");
	const state = replicatedState<ExtensionUIState>({
		revision: 0,
		prompt: null,
		statuses: {},
		notifications: [],
		widgets: {},
		workingVisible: true,
	});
	bridge.attachState(state);
	const result = bridge.ui.custom<string>((ui, _theme, keybindings, done) => {
		const editor = new CustomEditor(ui, getEditorTheme(), keybindings);
		editor.onSubmit = done;
		return editor;
	});
	await vi.waitFor(() => expect(state.value.prompt).toMatchObject({ kind: "custom" }));
	const id = state.value.prompt!.id;
	expect(await bridge.input(id, "extension choice")).toBe(true);
	expect(await bridge.input(id, "\r")).toBe(true);
	await expect(result).resolves.toBe("extension choice");
	expect(state.value.prompt).toBeNull();
	bridge.dispose();
});

test("before-agent-start hooks do not re-enter lane reads", async () => {
	const root = await mkdtemp("/tmp/pwh-");
	try {
		await mkdir(join(root, "extensions"));
		await writeFile(
			join(root, "extensions", "hook.ts"),
			`export default pi => pi.on("before_agent_start", event => { pi.appendEntry("pending", {}); return { systemPrompt: event.systemPrompt + " hooked" }; });`,
		);
		vi.stubEnv("PI_CODING_AGENT_DIR", root);
		const hooks = new Map<string, (event: never) => Promise<unknown>>();
		const findEntries = vi.fn(async () => []);
		const lane = {
			findEntries,
			appendCustomEntry: () => new Promise(() => {}),
			getActiveTools: async () => [],
			setActiveTools: async () => {},
		} as unknown as AgentLane;
		const harness = {
			getName: async () => undefined,
			setTools: async () => {},
			hooks: {
				on: (name: string, handler: (event: never) => Promise<unknown>) => {
					hooks.set(name, handler);
					return () => {};
				},
			},
			events: { on: () => () => {} },
		} as unknown as AgentHarness;
		const resources = await loadFullWorkerResources("/tmp");
		const deactivate = await resources.activate({
			harness,
			lane,
			metadata: {
				id: "hook",
				cwd: "/tmp",
				path: join(root, "hook.jsonl"),
				createdAt: 1,
				modifiedAt: 1,
				storageVersion: 1,
			},
		});
		const transform = hooks.get("transform_context");
		expect(transform).toBeDefined();
		await expect(
			transform!({ messages: [{ role: "user", content: "hello", timestamp: 1 }], systemPrompt: "base" } as never),
		).resolves.toMatchObject({ systemPrompt: "base hooked" });
		expect(findEntries).toHaveBeenCalledOnce();
		void deactivate;
	} finally {
		vi.unstubAllEnvs();
		await rm(root, { recursive: true, force: true });
	}
});

test("bridges extension autocomplete providers to the remote client", async () => {
	const root = await mkdtemp("/tmp/pwa-");
	try {
		await mkdir(join(root, "extensions"));
		await writeFile(
			join(root, "extensions", "autocomplete.ts"),
			`export default pi => pi.on("session_start", (_event, ctx) => ctx.ui.addAutocompleteProvider(current => ({
				triggerCharacters: ["/"],
				async getSuggestions(lines, cursorLine, cursorCol, options) {
					const before = (lines[cursorLine] ?? "").slice(0, cursorCol);
					if (!/(?:^|\\s)\\/[^\\s/]*$/.test(before) || before.startsWith("/")) return current.getSuggestions(lines, cursorLine, cursorCol, options);
					return { prefix: "/r", items: [{ value: "remote", label: "remote", description: "worker" }] };
				},
				applyCompletion(lines, cursorLine, cursorCol, item, prefix) {
					const next = [...lines];
					next[cursorLine] = next[cursorLine].slice(0, cursorCol - prefix.length) + "/" + item.value + next[cursorLine].slice(cursorCol);
					return { lines: next, cursorLine, cursorCol: cursorCol - prefix.length + item.value.length + 1 };
				},
			})));`,
		);
		vi.stubEnv("PI_CODING_AGENT_DIR", root);
		const resources = await loadFullWorkerResources("/tmp");
		const hooks = new Map<string, (event: never) => Promise<unknown>>();
		const lane = {
			findEntries: async () => [],
			getActiveTools: async () => [],
			setActiveTools: async () => {},
			setThinkingLevel: async () => {},
			getThinkingLevel: async () => "off",
		} as unknown as AgentLane;
		const harness = {
			getName: async () => undefined,
			setTools: async () => {},
			setName: async () => {},
			hooks: {
				on: (name: string, handler: (event: never) => Promise<unknown>) => {
					hooks.set(name, handler);
					return () => {};
				},
			},
			events: { on: () => () => {} },
		} as unknown as AgentHarness;
		const deactivate = await resources.activate({
			harness,
			lane,
			metadata: {
				id: "autocomplete",
				cwd: "/tmp",
				path: join(root, "autocomplete.jsonl"),
				createdAt: 1,
				modifiedAt: 1,
				storageVersion: 1,
			},
		});
		const result = await resources.extensionUI.autocomplete({
			lines: ["say /r"],
			cursorLine: 0,
			cursorCol: 6,
			baseSuggestions: { items: [{ value: "base", label: "base" }], prefix: "base" },
			baseCompletions: {
				"base\u0000base": { lines: ["say base"], cursorLine: 0, cursorCol: 8 },
			},
		});
		expect(result.suggestions).toEqual({
			items: [{ value: "remote", label: "remote", description: "worker" }],
			prefix: "/r",
		});
		expect(result.completions["remote\u0000remote"]).toEqual({
			lines: ["say /remote"],
			cursorLine: 0,
			cursorCol: 11,
		});
		expect(resources.extensionUI.getAutocompleteTriggerCharacters()).toEqual(["/"]);
		await deactivate();
	} finally {
		vi.unstubAllEnvs();
		await rm(root, { recursive: true, force: true });
	}
});

test("worker persists extension entries and exposes them after restart", async () => {
	const root = await mkdtemp("/tmp/pwr-");
	const agentDir = join(root, "agent");
	const sessionDir = join(root, "sessions");
	let server: RunningServer | undefined;
	let client: Awaited<ReturnType<typeof openClientRuntime>> | undefined;
	try {
		await configureExperimentalWorkerModel(agentDir);
		await mkdir(join(agentDir, "extensions"));
		await writeFile(
			join(agentDir, "extensions", "persist.ts"),
			`
export default function(pi) {
  pi.on("session_start", (_event, ctx) => {
    const saved = ctx.sessionManager.getBranch().find(entry => entry.type === "custom" && entry.customType === "fixture-state");
    pi.appendEntry(saved ? "fixture-restored" : "fixture-state", { count: saved ? saved.data.count + 1 : 1 });
  });
  pi.registerCommand("fixture-command", {
    description: "Persist command arguments",
    handler: async (args) => {
      pi.appendEntry("fixture-command", { args });
      pi.sendMessage({ customType: "fixture-message", content: args, display: true });
    },
  });
}
`,
		);
		vi.stubEnv("PI_CODING_AGENT_DIR", agentDir);
		vi.stubEnv("PI_OFFLINE", "1");
		await createExperimentalSessions(sessionDir, ["extension-persistence"]);
		server = await startServer({
			directory: join(root, "server"),
			sessionDir,
			provider: "anthropic",
			model: "claude-sonnet-4-5",
		});
		for (let index = 0; index < 2; index++) {
			client = await openClientRuntime({
				command: "client",
				connect: { transport: "unix", path: server.socketPath },
			});
			const services = await activateBuiltinClientServices(client.servers[0]!);
			await services.management.attach("extension-persistence", BACKGROUND_CONTEXT);
			const commandBinding = client.servers[0]!.session.open({
				services: [ExtensionCommands],
				assertAccess() {},
				onError() {},
			});
			await commandBinding.ready(BACKGROUND_CONTEXT);
			const commands = commandBinding.use(ExtensionCommands);
			expect(await commands.list(BACKGROUND_CONTEXT)).toContainEqual({
				name: "fixture-command",
				description: "Persist command arguments",
			});
			if (index === 0) await commands.run("fixture-command", "accepted", BACKGROUND_CONTEXT);
			await commandBinding.dispose(BACKGROUND_CONTEXT);
			const snapshot = await services.transcript.snapshot(BACKGROUND_CONTEXT);
			const customEntries = snapshot.transcript.filter((entry) => entry.type === "custom");
			expect(
				snapshot.transcript.filter((entry) => entry.type === "message" && entry.message.role === "custom"),
			).toMatchObject([{ message: { customType: "fixture-message", content: "accepted", display: true } }]);
			expect(customEntries).toMatchObject(
				index === 0
					? [
							{ customType: "fixture-state", data: { count: 1 } },
							{ customType: "fixture-command", data: { args: "accepted" } },
						]
					: [
							{ customType: "fixture-state", data: { count: 1 } },
							{ customType: "fixture-command", data: { args: "accepted" } },
							{ customType: "fixture-restored", data: { count: 2 } },
						],
			);
			await client.dispose();
			client = undefined;
			await expect.poll(() => server!.workerPids.size).toBe(0);
		}
	} finally {
		await client?.dispose();
		await server?.close();
		vi.unstubAllEnvs();
		await rm(root, { recursive: true, force: true });
	}
}, 15000);
