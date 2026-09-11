import { readFile } from "node:fs/promises";
import { dirname } from "node:path";
import type { Context, JsonValue, MutableReplicatedState } from "@earendil-works/chord";
import type {
	AgentHarness,
	AgentHarnessResources,
	AgentHarnessTool,
	AgentHarnessToolUpdateCallback,
	AgentLane,
	AgentMessage,
	Entry,
	JsonlSessionMetadata,
} from "@earendil-works/pi-agent-core";
import type {
	AutocompleteItem,
	AutocompleteProvider,
	AutocompleteSuggestions,
	Component,
	TUI,
} from "@earendil-works/pi-tui";
import { getAgentDir } from "../config.ts";
import type {
	EditorFactory,
	ExtensionCommandContext,
	ExtensionContext,
	ExtensionUIContext,
	LoadExtensionsResult,
	RegisteredCommand,
	ToolDefinition,
} from "../core/extensions/types.ts";
import { FooterDataProvider } from "../core/footer-data-provider.ts";
import { KeybindingsManager } from "../core/keybindings.ts";
import { ModelRegistry } from "../core/model-registry.ts";
import type { ModelRuntime } from "../core/model-runtime.ts";
import { DefaultResourceLoader } from "../core/resource-loader.ts";
import { SettingsManager } from "../core/settings-manager.ts";
import { buildSystemPrompt } from "../core/system-prompt.ts";
import { createCodingTools } from "../core/tools/index.ts";
import { getEditorTheme, initTheme, theme } from "../modes/interactive/theme/theme.ts";
import { stripFrontmatter } from "../utils/frontmatter.ts";
import type {
	ExtensionAutocompleteRequest,
	ExtensionAutocompleteResponse,
	ExtensionAutocompleteResult,
	ExtensionUIPrompt,
	ExtensionUIState,
} from "./services/extension-commands.ts";

export interface FullWorkerResources {
	readonly tools: AgentHarnessTool<object | undefined>[];
	readonly activeToolNames: string[];
	readonly resources: AgentHarnessResources;
	readonly systemPrompt: string;
	readonly diagnostics: readonly string[];
	readonly extensionUI: ExtensionUIBridge;
	readonly commands: {
		list(): readonly { name: string; description?: string }[];
		complete(
			name: string,
			prefix: string,
		): Promise<readonly { value: string; label: string; description?: string }[] | null>;
		run(name: string, args: string): Promise<readonly string[]>;
	};
	activate(options: {
		harness: AgentHarness;
		lane: AgentLane;
		metadata: JsonlSessionMetadata;
		reason?: "startup" | "reload" | "new" | "resume" | "fork";
	}): Promise<() => Promise<void>>;
}

function headlessValue(): unknown {
	const callable = () => undefined;
	return new Proxy(callable, {
		get: () => headlessValue(),
		apply: () => undefined,
	});
}

type RenderableExtensionComponent = Component & {
	handleInput?: (data: string) => void;
	isShowingAutocomplete?: () => boolean;
	onEscape?: (() => void) | undefined;
	onCtrlD?: (() => void) | undefined;
	onSubmit?: (text: string) => void;
	actionHandlers?: Map<string, () => void>;
	getText?: () => string;
	getExpandedText?: () => string;
	setText?: (text: string) => void;
	dispose?: () => void;
};

export interface ExtensionUIBridge {
	readonly ui: ExtensionUIContext;
	attachState(state: MutableReplicatedState<ExtensionUIState>): void;
	respond(id: string, value: unknown): Promise<void>;
	input(id: string, data: string): Promise<boolean>;
	autocomplete(request: ExtensionAutocompleteRequest): Promise<ExtensionAutocompleteResponse>;
	getAutocompleteTriggerCharacters(): readonly string[];
	setToolsExpanded(expanded: boolean): void;
	drainNotifications(): string[];
	dispose(): void;
}

function renderExtensionComponent(component: RenderableExtensionComponent): string[] {
	try {
		return component.render(80);
	} catch (error) {
		return [`Extension component render failed: ${error instanceof Error ? error.message : String(error)}`];
	}
}

export function createExtensionUIBridge(cwd: string, modelRegistry?: ModelRegistry): ExtensionUIBridge {
	let state: MutableReplicatedState<ExtensionUIState> | undefined;
	const footerData = new FooterDataProvider(cwd);
	if (modelRegistry !== undefined) {
		footerData.setAvailableProviderCount(new Set(modelRegistry.getAll().map((model) => model.provider)).size);
	}
	let revision = 0;
	let notificationId = 0;
	let notificationQueue: string[] = [];
	let nextPromptId = 0;
	type PendingPrompt = {
		kind: ExtensionUIPrompt["kind"];
		resolve: (value: unknown) => void;
		prompt?: ExtensionUIPrompt;
		component?: RenderableExtensionComponent;
	};
	const promptRequests = new Map<string, PendingPrompt>();
	const terminalInputHandlers = new Set<(data: string) => { consume?: boolean; data?: string } | undefined>();
	const widgets = new Map<
		string,
		{ content: string[]; placement: "aboveEditor" | "belowEditor"; component?: RenderableExtensionComponent }
	>();
	let editorComponent: RenderableExtensionComponent | undefined;
	let editorFactory: EditorFactory | undefined;
	const editorSubmitNamespace = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
	let editorSubmitId = 0;
	let editorSubmit: { id: string; text: string } | undefined;
	let workingMessage: string | undefined;
	let workingVisible = true;
	let workingIndicator: { frames?: string[]; intervalMs?: number } | undefined;
	let hiddenThinkingLabel: string | undefined;
	let toolsExpanded = false;
	let title: string | undefined;
	let footerComponent: RenderableExtensionComponent | undefined;
	let headerComponent: RenderableExtensionComponent | undefined;

	const snapshot = (): ExtensionUIState => ({
		revision,
		prompt: currentPrompt(),
		statuses: Object.fromEntries(statuses),
		notifications: [...notifications],
		...(terminalInputHandlers.size > 0 ? { terminalInput: true } : {}),
		widgets: Object.fromEntries(
			[...widgets].map(([key, value]) => [key, { content: value.content, placement: value.placement }]),
		),
		...(editorComponent === undefined ? {} : { editor: { lines: renderExtensionComponent(editorComponent) } }),
		...(editorSubmit === undefined ? {} : { editorSubmit }),
		...(workingMessage === undefined ? {} : { workingMessage }),
		workingVisible,
		...(workingIndicator === undefined ? {} : { workingIndicator }),
		...(hiddenThinkingLabel === undefined ? {} : { hiddenThinkingLabel }),
		toolsExpanded,
		...(title === undefined ? {} : { title }),
		...(footerComponent === undefined ? {} : { footer: renderExtensionComponent(footerComponent) }),
		...(headerComponent === undefined ? {} : { header: renderExtensionComponent(headerComponent) }),
	});
	const statuses = new Map<string, string>();
	const notifications: { id: number; message: string; type: "info" | "warning" | "error" }[] = [];
	const currentPrompt = (): ExtensionUIPrompt | null => {
		const first = promptRequests.values().next().value as PendingPrompt | undefined;
		const prompt = first?.prompt;
		if (!prompt) return null;
		if (prompt.kind === "custom" && first.component)
			return { ...prompt, lines: renderExtensionComponent(first.component) };
		return prompt;
	};
	const publish = (): void => {
		if (!state) return;
		revision += 1;
		const next = snapshot();
		const target = state.state as unknown as Record<string, unknown>;
		for (const key of Object.keys(target)) if (!Object.hasOwn(next, key)) delete target[key];
		Object.assign(target, next);
		try {
			state.publish(BACKGROUND_CONTEXT);
		} catch (error) {
			console.error("Extension UI state publish failed", error);
			throw error;
		}
	};
	footerData.onBranchChange(() => publish());
	const update = (operation: () => void): void => {
		operation();
		publish();
	};
	const fakeTui = new Proxy(
		{ requestRender: publish, terminal: { columns: 80, rows: 24 } },
		{ get: (target, property) => (property in target ? Reflect.get(target, property) : headlessValue()) },
	) as unknown as TUI;
	const keybindings = KeybindingsManager.create();
	const autocompleteFactories: ((current: AutocompleteProvider) => AutocompleteProvider)[] = [];
	const completionKey = (item: AutocompleteItem): string => `${item.value}\u0000${item.label}`;
	const createAutocompleteProvider = (request: ExtensionAutocompleteRequest): AutocompleteProvider => {
		const baseSuggestions: AutocompleteSuggestions | null =
			request.baseSuggestions === null
				? null
				: { items: [...request.baseSuggestions.items], prefix: request.baseSuggestions.prefix };
		const base: AutocompleteProvider = {
			getSuggestions: async () => baseSuggestions,
			applyCompletion: (lines, cursorLine, cursorCol, item) =>
				request.baseCompletions[completionKey(item)] ?? { lines, cursorLine, cursorCol },
			shouldTriggerFileCompletion: () => request.baseShouldTriggerFileCompletion ?? true,
		};
		let provider = base;
		for (const factory of autocompleteFactories) provider = factory(provider);
		return provider;
	};
	const request = <T>(
		kind: ExtensionUIPrompt["kind"],
		create: (id: string, resolve: (value: unknown) => void) => ExtensionUIPrompt,
	): Promise<T> => {
		const id = `extension-ui-${++nextPromptId}`;
		return new Promise<T>((resolve) => {
			const prompt = create(id, (value) => resolve(value as T));
			promptRequests.set(id, { kind, resolve: (value) => resolve(value as T), prompt });
			publish();
		});
	};
	const ui: ExtensionUIContext = {
		select: (promptTitle, options) =>
			request<string | undefined>("select", (id) => ({ id, kind: "select", title: promptTitle, options })),
		confirm: (promptTitle, message) =>
			request<boolean>("confirm", (id) => ({
				id,
				kind: "confirm",
				title: `${promptTitle}\n${message}`,
				options: ["Yes", "No"],
			})),
		input: (promptTitle, placeholder) =>
			request<string | undefined>("input", (id) => ({
				id,
				kind: "input",
				title: promptTitle,
				...(placeholder === undefined ? {} : { placeholder }),
			})),
		notify: (message, type = "info") => {
			notificationQueue.push(message);
			update(() => notifications.push({ id: ++notificationId, message, type }));
		},
		onTerminalInput: (handler) => {
			terminalInputHandlers.add(handler);
			publish();
			return () => {
				terminalInputHandlers.delete(handler);
				publish();
			};
		},
		setStatus: (key, text) =>
			update(() => {
				if (text === undefined) {
					statuses.delete(key);
					footerData.setExtensionStatus(key, undefined);
				} else {
					statuses.set(key, text);
					footerData.setExtensionStatus(key, text);
				}
			}),
		setWorkingMessage: (message) =>
			update(() => {
				workingMessage = message;
			}),
		setWorkingVisible: (visible) =>
			update(() => {
				workingVisible = visible;
			}),
		setWorkingIndicator: (options) =>
			update(() => {
				workingIndicator = options === undefined ? undefined : { ...options };
			}),
		setHiddenThinkingLabel: (label) =>
			update(() => {
				hiddenThinkingLabel = label;
			}),
		setWidget: (key, content, options) =>
			update(() => {
				const previous = widgets.get(key);
				previous?.component?.dispose?.();
				if (content === undefined) {
					widgets.delete(key);
					return;
				}
				if (Array.isArray(content)) {
					widgets.set(key, { content: [...content], placement: options?.placement ?? "aboveEditor" });
					return;
				}
				const component = content(fakeTui, theme);
				widgets.set(key, {
					content: renderExtensionComponent(component),
					placement: options?.placement ?? "aboveEditor",
					component,
				});
			}),
		setEditorComponent: (factory) =>
			update(() => {
				editorComponent?.dispose?.();
				editorFactory = factory;
				if (factory === undefined) {
					editorComponent = undefined;
					return;
				}
				editorComponent = factory(fakeTui, getEditorTheme(), keybindings) as RenderableExtensionComponent;
				editorComponent.onSubmit = (text) => {
					editorSubmit = { id: `${editorSubmitNamespace}-${++editorSubmitId}`, text };
					publish();
				};
			}),
		setFooter: (factory) =>
			update(() => {
				footerComponent?.dispose?.();
				footerComponent =
					factory === undefined
						? undefined
						: (factory(fakeTui, theme, footerData) as RenderableExtensionComponent);
			}),
		setHeader: (factory) =>
			update(() => {
				headerComponent?.dispose?.();
				headerComponent =
					factory === undefined ? undefined : (factory(fakeTui, theme) as RenderableExtensionComponent);
			}),
		setTitle: (value) =>
			update(() => {
				title = value;
			}),
		custom: async <T>(
			factory: Parameters<ExtensionUIContext["custom"]>[0],
			options?: Parameters<ExtensionUIContext["custom"]>[1],
		) => {
			const id = `extension-ui-${++nextPromptId}`;
			return await new Promise<T>((resolve, reject) => {
				const pending: PendingPrompt = {
					kind: "custom",
					resolve: (value: unknown) => resolve(value as T),
					prompt: { id, kind: "custom", lines: [], overlay: options?.overlay ?? false },
				};
				promptRequests.set(id, pending);
				Promise.resolve(
					factory(fakeTui, theme, keybindings, (value) => {
						void bridge.respond(id, value);
					}),
				)
					.then((component) => {
						if (!promptRequests.has(id)) {
							component.dispose?.();
							return;
						}
						pending.component = component as RenderableExtensionComponent;
						publish();
					})
					.catch((error) => {
						promptRequests.delete(id);
						publish();
						reject(error);
					});
				void options;
				publish();
			});
		},
		pasteToEditor: (text) => {
			editorComponent?.handleInput?.(`\x1b[200~${text}\x1b[201~`);
			publish();
		},
		setEditorText: (text) => {
			editorComponent?.setText?.(text);
			publish();
		},
		getEditorText: () => editorComponent?.getExpandedText?.() ?? "",
		editor: (promptTitle, prefill) =>
			request<string | undefined>("editor", (id) => ({
				id,
				kind: "editor",
				title: promptTitle,
				...(prefill === undefined ? {} : { initial: prefill }),
			})),
		addAutocompleteProvider: (factory) => {
			autocompleteFactories.push(factory);
		},
		getEditorComponent: () => editorFactory,
		get theme() {
			return theme;
		},
		getAllThemes: () => [],
		getTheme: () => undefined,
		setTheme: () => ({ success: false, error: "Themes are controlled by the viewing client" }),
		getToolsExpanded: () => toolsExpanded,
		setToolsExpanded: (expanded) =>
			update(() => {
				toolsExpanded = expanded;
			}),
	};
	const bridge: ExtensionUIBridge = {
		ui,
		attachState(nextState) {
			state = nextState;
			publish();
		},
		async respond(id, value) {
			const pending = promptRequests.get(id);
			if (!pending) return;
			promptRequests.delete(id);
			pending.component?.dispose?.();
			pending.resolve(value === null && pending.kind === "confirm" ? false : value === null ? undefined : value);
			publish();
		},
		async input(id, data) {
			const pending = promptRequests.get(id);
			if (pending?.component?.handleInput) {
				pending.component.handleInput(data);
				publish();
				return true;
			}
			if (editorComponent?.handleInput) {
				const interrupt = keybindings.matches(data, "app.interrupt");
				const autocompleteActive = editorComponent.isShowingAutocomplete?.() ?? false;
				editorComponent.handleInput(data);
				publish();
				if (interrupt && !autocompleteActive && editorComponent.onEscape === undefined) return false;
				if (keybindings.matches(data, "app.clear") && !editorComponent.actionHandlers?.has("app.clear"))
					return false;
				if (
					keybindings.matches(data, "app.exit") &&
					editorComponent.getText?.().length === 0 &&
					editorComponent.onCtrlD === undefined &&
					!editorComponent.actionHandlers?.has("app.exit")
				)
					return false;
				return true;
			}
			let consumed = false;
			for (const handler of terminalInputHandlers) {
				const result = handler(data);
				if (result?.consume) {
					consumed = true;
					break;
				}
			}
			return consumed;
		},
		async autocomplete(request) {
			const provider = createAutocompleteProvider(request);
			const suggestions = await provider.getSuggestions(request.lines, request.cursorLine, request.cursorCol, {
				signal: new AbortController().signal,
				force: request.force,
			});
			const completions: Record<string, ExtensionAutocompleteResult> = {};
			if (suggestions !== null) {
				for (const item of suggestions.items) {
					completions[completionKey(item)] = provider.applyCompletion(
						request.lines,
						request.cursorLine,
						request.cursorCol,
						item,
						suggestions.prefix,
					);
				}
			}
			return {
				suggestions: suggestions === null ? null : { items: suggestions.items, prefix: suggestions.prefix },
				completions,
				...(provider.shouldTriggerFileCompletion === undefined
					? {}
					: {
							shouldTriggerFileCompletion: provider.shouldTriggerFileCompletion(
								request.lines,
								request.cursorLine,
								request.cursorCol,
							),
						}),
			};
		},
		getAutocompleteTriggerCharacters() {
			const provider = createAutocompleteProvider({
				lines: [],
				cursorLine: 0,
				cursorCol: 0,
				baseSuggestions: null,
				baseCompletions: {},
			});
			return [...(provider.triggerCharacters ?? [])];
		},
		setToolsExpanded(expanded) {
			update(() => {
				toolsExpanded = expanded;
			});
		},
		drainNotifications() {
			const result = notificationQueue;
			notificationQueue = [];
			return result;
		},
		dispose() {
			for (const pending of promptRequests.values()) {
				pending.component?.dispose?.();
				pending.resolve(undefined);
			}
			promptRequests.clear();
			for (const widget of widgets.values()) widget.component?.dispose?.();
			widgets.clear();
			editorComponent?.dispose?.();
			editorComponent = undefined;
			editorFactory = undefined;
			footerComponent?.dispose?.();
			footerComponent = undefined;
			headerComponent?.dispose?.();
			headerComponent = undefined;
			terminalInputHandlers.clear();
			footerData.dispose();
		},
	};
	return bridge;
}

function messageText(message: { content?: unknown }): string {
	if (typeof message.content === "string") return message.content;
	if (!Array.isArray(message.content)) return "";
	return message.content
		.map((part) =>
			part && typeof part === "object" && "text" in part && typeof part.text === "string" ? part.text : "",
		)
		.filter(Boolean)
		.join("\n");
}

function adaptTool(
	definition: ToolDefinition,
	cwd: string,
	contextRef: { current?: ExtensionContext },
): AgentHarnessTool<object | undefined> {
	return {
		name: definition.name,
		label: definition.label,
		description: definition.description,
		parameters: definition.parameters,
		...(definition.constrainedSampling === undefined ? {} : { constrainedSampling: definition.constrainedSampling }),
		async execute(toolCallId, params, onUpdate, _toolContext, _invocation, context: Context) {
			const signal = context.abortSignal;
			const extensionContext =
				contextRef.current ??
				(new Proxy(
					{ cwd, mode: "rpc", hasUI: false, signal, ui: headlessValue() },
					{ get: (target, property) => Reflect.get(target, property) ?? headlessValue() },
				) as ExtensionContext);
			return definition.execute(
				toolCallId,
				params,
				signal,
				onUpdate as AgentHarnessToolUpdateCallback<unknown>,
				extensionContext,
			);
		},
	} as AgentHarnessTool<object | undefined>;
}

async function createExtensionContext(options: {
	cwd: string;
	harness: AgentHarness;
	lane: AgentLane;
	metadata: JsonlSessionMetadata;
	extensions: LoadExtensionsResult;
	activeToolNames: string[];
	harnessSkills: AgentHarnessResources["skills"];
	systemPrompt: string;
	extensionUI: ExtensionUIBridge;
	modelRegistry?: ModelRegistry;
	systemPromptRef: { current: string };
}): Promise<{
	context: ExtensionContext;
	refresh(): Promise<void>;
	flush(): Promise<void>;
	drainNotifications(): string[];
	recordEntry(entry: Awaited<ReturnType<AgentLane["findEntry"]>>): void;
	getEntry(id: string): Entry | undefined;
	getEntries(): Entry[];
	setModelIdentity(value: { provider: string; modelId: string }): void;
	setThinkingLevel(value: Awaited<ReturnType<AgentLane["getThinkingLevel"]>>): void;
	getMessages(): AgentMessage[];
	setIdle(value: boolean): void;
}> {
	const {
		cwd,
		harness,
		lane,
		metadata,
		extensions,
		activeToolNames,
		harnessSkills,
		extensionUI,
		modelRegistry,
		systemPromptRef,
	} = options;
	let active = [...activeToolNames];
	let idle = true;
	let entries = await lane.findEntries({ order: "oldestFirst" }, BACKGROUND_CONTEXT);
	let sessionName = await harness.getName(BACKGROUND_CONTEXT);
	const getModel = (lane as unknown as { getModel?: AgentLane["getModel"] }).getModel;
	let currentModel: unknown = getModel === undefined ? undefined : await getModel.call(lane, BACKGROUND_CONTEXT);
	const getThinkingLevel = (lane as unknown as { getThinkingLevel?: AgentLane["getThinkingLevel"] }).getThinkingLevel;
	let thinkingLevel =
		getThinkingLevel === undefined ? ("off" as const) : await getThinkingLevel.call(lane, BACKGROUND_CONTEXT);
	const pendingWrites: Promise<{ error?: unknown }>[] = [];
	const enqueueWrite = (write: Promise<unknown>): void => {
		pendingWrites.push(
			write.then(
				() => ({}),
				(error: unknown) => ({ error }),
			),
		);
	};
	const refresh = async (): Promise<void> => {
		entries = await lane.findEntries({ order: "oldestFirst" }, BACKGROUND_CONTEXT);
		sessionName = await harness.getName(BACKGROUND_CONTEXT);
	};
	const flush = async (): Promise<void> => {
		if (pendingWrites.length === 0) return;
		const results = await Promise.all(pendingWrites.splice(0));
		const failures = results.filter((result) => "error" in result);
		if (failures.length > 0)
			throw new AggregateError(
				failures.map((result) => result.error),
				"Extension state writes failed",
			);
	};
	const branchEntries = () =>
		entries.map((entry) => ({ ...entry, timestamp: new Date(entry.timestamp).toISOString() }));
	const sessionRoot = dirname(dirname(metadata.path));
	const sessionManager = {
		getSessionId: () => metadata.id,
		getSessionFile: () => metadata.path,
		getSessionDir: () => sessionRoot,
		getCwd: () => metadata.cwd,
		getSessionName: () => sessionName,
		getHeader: () => ({ id: metadata.id, cwd: metadata.cwd, timestamp: new Date(metadata.createdAt).toISOString() }),
		getEntries: branchEntries,
		getBranch: branchEntries,
		getLeafId: () => entries.at(-1)?.id ?? null,
		getEntry: (id: string) => branchEntries().find((entry) => entry.id === id),
		usesDefaultSessionDir: () => false,
	};
	const ui = extensionUI.ui;
	const context = new Proxy(
		{
			cwd,
			mode: "tui" as const,
			hasUI: true,
			ui,
			sessionManager,
			modelRegistry: modelRegistry ?? headlessValue(),
			model: currentModel,
			scopedModels: [],
			thinkingLevel,
			isIdle: () => idle,
			isProjectTrusted: () => true,
			signal: undefined,
			abort: () => void lane.abort(BACKGROUND_CONTEXT),
			hasPendingMessages: () => false,
			shutdown: () => undefined,
			getContextUsage: () => undefined,
			waitForIdle: () => lane.waitForIdle(BACKGROUND_CONTEXT),
			compact: () => void lane.compact(undefined, BACKGROUND_CONTEXT),
			getSystemPrompt: () => systemPromptRef.current,
			getSystemPromptOptions: () => ({ cwd, skills: harnessSkills }),
		},
		{
			get: (target, property) => {
				if (property === "model") return currentModel;
				if (property === "thinkingLevel") return thinkingLevel;
				return Reflect.get(target, property) ?? headlessValue();
			},
		},
	) as unknown as ExtensionContext;

	const runtime = extensions.runtime;
	runtime.getSessionName = () => sessionName;
	runtime.getThinkingLevel = () => thinkingLevel;
	runtime.setThinkingLevel = (level) => {
		thinkingLevel = level;
		enqueueWrite(lane.setThinkingLevel(level, BACKGROUND_CONTEXT));
	};
	runtime.setModel = async (model) => {
		await lane.setModel({ provider: model.provider, modelId: model.id }, BACKGROUND_CONTEXT);
		currentModel = model;
		return true;
	};
	runtime.setSessionName = (name) => {
		sessionName = name;
		extensionUI.ui.setTitle(name);
		enqueueWrite(harness.setName(name, BACKGROUND_CONTEXT));
	};
	runtime.getActiveTools = () => [...active];
	runtime.setActiveTools = (names) => {
		active = [...names];
		void lane.setActiveTools(active, BACKGROUND_CONTEXT);
	};
	runtime.refreshTools = () => undefined;
	runtime.getAllTools = () =>
		extensions.extensions.flatMap((extension) =>
			[...extension.tools.values()].map(({ definition }) => ({
				name: definition.name,
				description: definition.description,
				parameters: definition.parameters,
				promptSnippet: definition.promptSnippet,
				promptGuidelines: definition.promptGuidelines,
				sourceInfo: extension.sourceInfo,
			})),
		);
	runtime.sendMessage = async (message, sendOptions) => {
		const nativeMessage = {
			role: "custom" as const,
			customType: message.customType,
			content: message.content,
			display: message.display,
			...(message.details === undefined
				? {}
				: { details: JSON.parse(JSON.stringify(message.details)) as JsonValue }),
			timestamp: Date.now(),
		};
		const active = !idle;
		if (!active && sendOptions?.triggerTurn) {
			await lane.prompt(nativeMessage, BACKGROUND_CONTEXT);
			return;
		}
		if (!active) {
			await lane.appendMessage(nativeMessage, BACKGROUND_CONTEXT);
			return;
		}
		const delivery = sendOptions?.deliverAs ?? "steer";
		if (delivery === "steer") await lane.steer(nativeMessage, undefined, BACKGROUND_CONTEXT);
		else if (delivery === "nextTurn") await lane.nextRun(nativeMessage, undefined, BACKGROUND_CONTEXT);
		else await lane.followUp(nativeMessage, undefined, BACKGROUND_CONTEXT);
	};
	runtime.sendUserMessage = async (content, sendOptions) => {
		const message = {
			role: "user" as const,
			content: typeof content === "string" ? [{ type: "text" as const, text: content }] : content,
			timestamp: Date.now(),
		};
		if (idle) await lane.prompt(message, BACKGROUND_CONTEXT);
		else if (sendOptions?.deliverAs === "steer") await lane.steer(message, undefined, BACKGROUND_CONTEXT);
		else await lane.followUp(message, undefined, BACKGROUND_CONTEXT);
	};
	runtime.appendEntry = (customType, data) => {
		const serialized = data === undefined ? undefined : JSON.stringify(data);
		const value = serialized === undefined ? undefined : (JSON.parse(serialized) as JsonValue);
		enqueueWrite(lane.appendCustomEntry(customType, value, BACKGROUND_CONTEXT));
	};
	runtime.setLabel = (entryId, label) => void harness.setLabel(entryId, label, BACKGROUND_CONTEXT);
	runtime.getCommands = () =>
		extensions.extensions.flatMap((extension) =>
			[...extension.commands.values()].map((command) => ({
				name: command.name,
				description: command.description,
				source: "extension" as const,
				sourceInfo: extension.sourceInfo,
			})),
		);
	return {
		context,
		refresh,
		flush,
		drainNotifications() {
			return extensionUI.drainNotifications();
		},
		recordEntry(entry) {
			if (entry && !entries.some((candidate) => candidate.id === entry.id)) entries = [...entries, entry];
		},
		getEntry(id) {
			return entries.find((entry) => entry.id === id);
		},
		getEntries() {
			return [...entries];
		},
		setModelIdentity(value) {
			if (currentModel && typeof currentModel === "object") {
				currentModel = {
					...(currentModel as Record<string, unknown>),
					provider: value.provider,
					id: value.modelId,
				};
			} else currentModel = value;
		},
		setThinkingLevel(value) {
			thinkingLevel = value;
		},
		getMessages() {
			return entries.flatMap((entry) => (entry.type === "message" ? [entry.message] : []));
		},
		setIdle(value) {
			idle = value;
		},
	};
}

// Kept here to avoid coupling legacy ExtensionContext to Chord's Context type.
const BACKGROUND_CONTEXT = {
	abortSignal: undefined,
	value: () => undefined,
	toString: () => "full-worker-extension",
} as Context;

/** Load the same filesystem resources as normal Pi for an experimental v4 Session worker. */
export async function loadFullWorkerResources(
	cwd: string,
	allowSubagents = true,
	extensionUI?: ExtensionUIBridge,
	modelRuntime?: ModelRuntime,
): Promise<FullWorkerResources> {
	const agentDir = getAgentDir();
	const settingsManager = SettingsManager.create(cwd, agentDir);
	initTheme(settingsManager.getTheme(), false);
	const loader = new DefaultResourceLoader({ cwd, agentDir, settingsManager });
	await loader.reload();
	const modelRegistry = modelRuntime === undefined ? undefined : new ModelRegistry(modelRuntime);
	if (modelRuntime !== undefined) {
		for (const registration of loader.getExtensions().runtime.pendingProviderRegistrations) {
			modelRuntime.registerProvider(registration.name, registration.config);
		}
		for (const registration of loader.getExtensions().runtime.pendingNativeProviderRegistrations) {
			modelRuntime.registerNativeProvider(registration.provider);
		}
	}

	const definitions = new Map<string, ToolDefinition>();
	for (const definition of createCodingTools(cwd)) definitions.set(definition.name, definition);
	const extensions = loader.getExtensions();
	for (const extension of extensions.extensions) {
		for (const { definition } of extension.tools.values()) definitions.set(definition.name, definition);
	}

	const activeToolNames = [...definitions.keys()];
	const toolSnippets: Record<string, string> = {};
	const promptGuidelines: string[] = [];
	for (const definition of definitions.values()) {
		if (definition.promptSnippet) toolSnippets[definition.name] = definition.promptSnippet;
		if (definition.promptGuidelines) promptGuidelines.push(...definition.promptGuidelines);
	}

	const skills = loader.getSkills();
	const workerSkills = skills.skills.filter(
		(skill) => skill.name !== "secretary" && (allowSubagents || skill.name !== "subagent"),
	);
	const prompts = loader.getPrompts();
	const harnessSkills = await Promise.all(
		workerSkills.map(async (skill) => ({
			name: skill.name,
			description: skill.description,
			content: stripFrontmatter(await readFile(skill.filePath, "utf8")).trim(),
			filePath: skill.filePath,
			disableModelInvocation: skill.disableModelInvocation,
		})),
	);
	const loaderAppendSystemPrompt = loader.getAppendSystemPrompt();
	const systemPrompt = buildSystemPrompt({
		cwd,
		customPrompt: loader.getSystemPrompt(),
		appendSystemPrompt: loaderAppendSystemPrompt.length > 0 ? loaderAppendSystemPrompt.join("\n\n") : undefined,
		contextFiles: loader.getAgentsFiles().agentsFiles,
		skills: workerSkills,
		selectedTools: activeToolNames,
		toolSnippets,
		promptGuidelines,
	});
	extensionUI ??= createExtensionUIBridge(cwd, modelRegistry);
	const systemPromptRef = { current: systemPrompt };
	const contextRef: { current?: ExtensionContext } = {};
	const flushRef: { current?: () => Promise<void> } = {};
	const notificationRef: { current?: () => string[] } = {};
	const registeredCommands = (): { invocationName: string; command: RegisteredCommand }[] => {
		const commands = extensions.extensions.flatMap((extension) => [...extension.commands.values()]);
		const counts = new Map<string, number>();
		for (const command of commands) counts.set(command.name, (counts.get(command.name) ?? 0) + 1);
		const seen = new Map<string, number>();
		return commands.map((command) => {
			const occurrence = (seen.get(command.name) ?? 0) + 1;
			seen.set(command.name, occurrence);
			return {
				invocationName: (counts.get(command.name) ?? 0) > 1 ? `${command.name}:${occurrence}` : command.name,
				command,
			};
		});
	};

	return {
		extensionUI,
		tools: [...definitions.values()].map((definition) => adaptTool(definition, cwd, contextRef)),
		activeToolNames,
		resources: { skills: harnessSkills, promptTemplates: prompts.prompts },
		systemPrompt,
		commands: {
			list: () =>
				registeredCommands().map(({ invocationName, command }) => ({
					name: invocationName,
					...(command.description === undefined ? {} : { description: command.description }),
				})),
			async complete(name, prefix) {
				const command = registeredCommands().find((entry) => entry.invocationName === name)?.command;
				if (!command?.getArgumentCompletions) return null;
				return (
					(await command.getArgumentCompletions(prefix))?.map((item) => ({
						value: item.value,
						label: item.label,
						...(item.description === undefined ? {} : { description: item.description }),
					})) ?? null
				);
			},
			async run(name, args) {
				const command = registeredCommands().find((entry) => entry.invocationName === name)?.command;
				if (!command) throw new Error(`Unknown extension command: /${name}`);
				if (!contextRef.current) throw new Error("Extension runtime is not active");
				try {
					await command.handler(args, contextRef.current as ExtensionCommandContext);
					await flushRef.current?.();
					return notificationRef.current?.() ?? [];
				} catch (error) {
					console.error(`Extension command /${name} failed`, error);
					throw error;
				}
			},
		},
		diagnostics: [
			...extensions.errors.map(({ path, error }) => `${path}: ${error}`),
			...skills.diagnostics.map(({ path, message }) => `${path}: ${message}`),
			...prompts.diagnostics.map(({ path, message }) => `${path}: ${message}`),
		],
		async activate({ harness, lane, metadata, reason = "startup" }) {
			const extensionState = await createExtensionContext({
				cwd,
				harness,
				lane,
				metadata,
				extensions,
				activeToolNames,
				harnessSkills,
				systemPrompt,
				extensionUI,
				modelRegistry,
				systemPromptRef,
			});
			const { context } = extensionState;
			contextRef.current = context;
			flushRef.current = extensionState.flush;
			notificationRef.current = extensionState.drainNotifications;
			const emit = async (name: string, event: unknown): Promise<unknown[]> => {
				const results: unknown[] = [];
				for (const extension of extensions.extensions) {
					for (const handler of extension.handlers.get(name) ?? []) {
						try {
							results.push(await handler(event, context));
						} catch (error) {
							console.error(`Extension ${extension.path} ${name} error:`, error);
						}
					}
				}
				return results;
			};
			const emitMessageEnd = async (message: AgentMessage): Promise<AgentMessage> => {
				let current = message;
				for (const extension of extensions.extensions) {
					for (const handler of extension.handlers.get("message_end") ?? []) {
						try {
							const result = (await handler({ type: "message_end", message: current }, context)) as
								| { message?: AgentMessage }
								| undefined;
							if (result?.message?.role === current.role) current = result.message;
						} catch (error) {
							console.error(`Extension ${extension.path} message_end error:`, error);
						}
					}
				}
				return current;
			};
			await emit("session_start", { type: "session_start", reason });
			await extensionState.flush();
			const refreshedDefinitions = new Map<string, ToolDefinition>();
			for (const definition of createCodingTools(cwd)) refreshedDefinitions.set(definition.name, definition);
			for (const extension of extensions.extensions) {
				for (const { definition } of extension.tools.values())
					refreshedDefinitions.set(definition.name, definition);
			}
			await harness.setTools(
				[...refreshedDefinitions.values()].map((definition) => adaptTool(definition, cwd, contextRef)),
				BACKGROUND_CONTEXT,
			);
			const available = new Set(refreshedDefinitions.keys());
			const configured = await lane.getActiveTools(BACKGROUND_CONTEXT);
			const validConfigured = configured.filter((name) => available.has(name));
			if (validConfigured.length !== configured.length) {
				await lane.setActiveTools(validConfigured, BACKGROUND_CONTEXT);
			}

			let preparedBeforeAgentPrompt: string | undefined;
			const beforeAgentStart = async (
				messages: readonly AgentMessage[],
				eventSystemPrompt?: string,
			): Promise<AgentMessage[]> => {
				if (typeof eventSystemPrompt === "string") systemPromptRef.current = eventSystemPrompt;
				let userIndex = -1;
				for (let index = messages.length - 1; index >= 0; index--) {
					if (messages[index]?.role === "user") {
						userIndex = index;
						break;
					}
				}
				const user = userIndex === -1 ? undefined : messages[userIndex];
				const prompt = user ? messageText(user as { content?: unknown }) : "";
				const results = await emit("before_agent_start", {
					type: "before_agent_start",
					prompt,
					systemPrompt: systemPromptRef.current,
					systemPromptOptions: { cwd, skills: harnessSkills },
				});
				return results.flatMap((result): AgentMessage[] => {
					const value = result as { message?: Record<string, unknown>; systemPrompt?: string } | undefined;
					if (value?.systemPrompt !== undefined) systemPromptRef.current = value.systemPrompt;
					if (
						!value?.message ||
						typeof value.message.customType !== "string" ||
						!Array.isArray(value.message.content) ||
						typeof value.message.display !== "boolean"
					)
						return [];
					return [
						{
							role: "custom" as const,
							customType: value.message.customType,
							content: value.message.content,
							display: value.message.display,
							...(value.message.details === undefined ? {} : { details: value.message.details }),
							timestamp: Date.now(),
						} as AgentMessage,
					];
				});
			};
			harness.hooks.on("before_run", async (event) => {
				const injected = await beforeAgentStart(event.prompt);
				let userIndex = -1;
				for (let index = event.prompt.length - 1; index >= 0; index--) {
					if (event.prompt[index]?.role === "user") {
						userIndex = index;
						break;
					}
				}
				const user = userIndex === -1 ? undefined : event.prompt[userIndex];
				preparedBeforeAgentPrompt = user ? messageText(user as { content?: unknown }) : "";
				return injected.length === 0 ? undefined : { messages: injected };
			});
			harness.hooks.on("transform_context", async (event) => {
				let messages = event.messages;
				if (typeof event.systemPrompt === "string") systemPromptRef.current = event.systemPrompt;
				let promptUserIndex = -1;
				for (let index = messages.length - 1; index >= 0; index--) {
					if (messages[index]?.role === "user") {
						promptUserIndex = index;
						break;
					}
				}
				const promptUser = promptUserIndex === -1 ? undefined : messages[promptUserIndex];
				const prompt = promptUser ? messageText(promptUser as { content?: unknown }) : "";
				let injected: AgentMessage[] = [];
				if (preparedBeforeAgentPrompt === prompt) preparedBeforeAgentPrompt = undefined;
				else injected = await beforeAgentStart(messages, event.systemPrompt);
				if (injected.length > 0) messages = [...messages, ...injected];
				let userIndex = -1;
				for (let index = messages.length - 1; index >= 0; index--) {
					if (messages[index]?.role === "user") {
						userIndex = index;
						break;
					}
				}
				const user = userIndex === -1 ? undefined : messages[userIndex];
				if (user && user.role === "user") {
					const inputResult = (
						await emit("input", {
							type: "input",
							text: messageText(user as { content?: unknown }),
							source: "rpc",
						})
					)
						.map((result) => result as { action?: string; text?: string } | undefined)
						.find((result) => result?.action === "transform");
					const transformedText = inputResult?.text;
					if (typeof transformedText === "string") {
						messages = messages.map((message, index) =>
							index === userIndex && message.role === "user"
								? { ...message, content: [{ type: "text", text: transformedText }] }
								: message,
						);
					}
				}
				for (const result of await emit("context", { type: "context", messages })) {
					const replacement = result as { messages?: typeof messages } | undefined;
					if (replacement?.messages) messages = replacement.messages;
				}
				return { messages, systemPrompt: systemPromptRef.current };
			});
			harness.hooks.on("before_compaction", async (event) => {
				for (const result of await emit("session_before_compact", {
					type: "session_before_compact",
					reason: event.reason,
					preparation: event.preparation,
					branchEntries: extensionState.getEntries().map((entry) => ({
						...entry,
						timestamp: new Date(entry.timestamp).toISOString(),
					})),
					customInstructions: event.customInstructions,
					willRetry: event.reason === "overflow",
					signal: context.signal,
				})) {
					const decision = result as { cancel?: boolean; compaction?: unknown } | undefined;
					if (decision?.cancel) return { decline: true };
					if (decision?.compaction) return { compaction: decision.compaction as never };
				}
				return undefined;
			});
			harness.hooks.on("before_navigation", async (event) => {
				for (const result of await emit("session_before_tree", {
					type: "session_before_tree",
					preparation: event.preparation,
					customInstructions: event.customInstructions,
					signal: context.signal,
				})) {
					const decision = result as { cancel?: boolean; summary?: unknown } | undefined;
					if (decision?.cancel) return { decline: true };
					if (decision?.summary) return { summary: decision.summary as never };
				}
				return undefined;
			});
			harness.hooks.on("after_response", async (event) => {
				const message = await emitMessageEnd(event.message);
				return message === event.message ? undefined : { message: message as typeof event.message };
			});
			harness.hooks.on("before_tool", async (event) => {
				for (const result of await emit("tool_call", {
					type: "tool_call",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					input: event.args,
				})) {
					const decision = result as { block?: boolean; reason?: string } | undefined;
					if (decision?.block) return { block: { reason: decision.reason ?? "Blocked by extension" } };
				}
				return undefined;
			});
			harness.hooks.on("after_tool", async (event) => {
				let patch: Record<string, unknown> | undefined;
				for (const result of await emit("tool_result", {
					type: "tool_result",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					input: event.args,
					content: event.content,
					details: event.details,
					isError: event.isError,
				})) {
					if (result) patch = { ...(patch ?? {}), ...(result as Record<string, unknown>) };
				}
				return patch as never;
			});

			harness.events.on("compaction_end", async (event) => {
				if (event.status === "completed") {
					await emit("session_compact", {
						type: "session_compact",
						compactionEntry: extensionState.getEntry(event.entryId) ?? null,
						fromExtension: false,
						reason: event.reason,
						willRetry: event.reason === "overflow",
					});
				} else {
					await emit("session_compact_failed", {
						type: "session_compact_failed",
						reason: event.reason,
						aborted: event.status === "aborted",
						...(event.status === "failed" ? { errorMessage: event.error.message } : {}),
						willRetry: event.reason === "overflow",
						fromExtension: false,
					});
				}
			});
			harness.events.on("config_update", async (event) => {
				if (event.property === "model") {
					extensionState.setModelIdentity(event.value);
					await emit("model_select", {
						type: "model_select",
						model: event.value,
						previousModel: event.previous,
						source: "set",
					});
				} else if (event.property === "thinkingLevel") {
					extensionState.setThinkingLevel(event.value);
					await emit("thinking_level_select", {
						type: "thinking_level_select",
						level: event.value,
						previousLevel: event.previous,
					});
				}
			});
			harness.events.on("entry_added", (event) => {
				extensionState.recordEntry(event.entry);
			});
			harness.events.on("value_update", async (event) => {
				if (event.value === "session_name") {
					await emit("session_info_changed", { type: "session_info_changed", name: event.name });
				}
			});
			let turnIndex = 0;
			let currentTurnIndex = 0;
			harness.events.on("run_start", async () => {
				turnIndex = 0;
				extensionState.setIdle(false);
				await emit("agent_start", { type: "agent_start" });
			});
			harness.events.on("run_end", async (event) => {
				try {
					await emit("agent_end", {
						type: "agent_end",
						messages: extensionState.getMessages(),
					});
					await emit("agent_settled", { type: "agent_settled" });
				} finally {
					extensionState.setIdle(true);
				}
				void event;
			});
			harness.events.on("turn_start", async (event) => {
				currentTurnIndex = turnIndex++;
				await emit("turn_start", { type: "turn_start", turnIndex: currentTurnIndex, timestamp: Date.now() });
				void event;
			});
			harness.events.on("turn_end", async (event) => {
				await emit("turn_end", {
					type: "turn_end",
					turnIndex: currentTurnIndex,
					message: event.message,
					toolResults: event.toolResults,
				});
			});
			const toolArguments = new Map<string, unknown>();
			harness.events.on("message_start", async (event) => {
				await emit("message_start", event);
			});
			harness.events.on("message_update", async (event) => {
				await emit("message_update", {
					type: "message_update",
					message: event.message,
					assistantMessageEvent: event.event,
				});
			});
			harness.events.on("message_end", async (event) => {
				if (event.message.role !== "assistant") await emit("message_end", event);
			});
			harness.events.on("tool_start", async (event) => {
				toolArguments.set(event.toolCallId, event.args);
				await emit("tool_execution_start", {
					type: "tool_execution_start",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
				});
			});
			harness.events.on("tool_update", async (event) => {
				await emit("tool_execution_update", {
					type: "tool_execution_update",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: toolArguments.get(event.toolCallId) ?? {},
					partialResult: event.partialResult,
				});
			});
			harness.events.on("tool_end", async (event) => {
				const args = toolArguments.get(event.toolCallId) ?? {};
				toolArguments.delete(event.toolCallId);
				await emit("tool_execution_end", {
					type: "tool_execution_end",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args,
					result: event.result,
					isError: event.isError,
				});
			});
			harness.events.on("navigation_end", async (event) => {
				if (event.status === "completed") {
					await emit("session_tree", {
						type: "session_tree",
						newLeafId: event.tipId,
						oldLeafId: event.fromTipId,
						fromExtension: false,
					});
				}
			});
			return async () => {
				await emit("session_shutdown", { type: "session_shutdown" });
				await extensionState.flush();
				contextRef.current = undefined;
				flushRef.current = undefined;
				notificationRef.current = undefined;
				extensionUI.dispose();
			};
		},
	};
}
